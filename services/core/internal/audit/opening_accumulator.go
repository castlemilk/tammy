package audit

import (
	"bytes"
	"container/heap"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"io"
	"os"
	"sort"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

const (
	externalOpeningRecordSize          = sha256.Size + 8 + sha256.Size
	ExternalOpeningRecordLimit  uint64 = 5_000_000
	externalOpeningChunkRecords        = 65_536
	externalOpeningMergeFanIn          = 32
)

type externalOpeningAccumulatorHooks struct {
	CreateTemp func(string, string) (*os.File, error)
	WriteAll   func(io.Writer, []byte) error
	ReadFull   func(io.Reader, []byte) (int, error)
	Close      func(*os.File) error
}

type externalOpeningAccumulatorConfig struct {
	Context         context.Context
	ParentDirectory string
	RecordLimit     uint64
	ChunkRecords    int
	MergeFanIn      int
	Hooks           externalOpeningAccumulatorHooks
}

type externalOpeningAccumulator struct {
	ctx                     context.Context
	directory               string
	unsorted                *os.File
	recordLimit             uint64
	recordCount             uint64
	chunkRecords            int
	mergeFanIn              int
	hooks                   externalOpeningAccumulatorHooks
	maxChunkRecordsObserved int
	maxMergeFanInObserved   int
	finished                bool
	closed                  bool
	terminalErr             error
	duplicateSequence       uint64
	headBeforeDuplicate     [sha256.Size]byte
}

type externalOpeningRecord struct {
	digest     [sha256.Size]byte
	sequence   uint64
	headBefore [sha256.Size]byte
}

func newExternalOpeningAccumulator(config externalOpeningAccumulatorConfig) (*externalOpeningAccumulator, error) {
	if config.Context == nil || config.RecordLimit > ExternalOpeningRecordLimit ||
		config.ChunkRecords <= 0 || config.MergeFanIn < 2 {
		return nil, ErrInvalidChainInput
	}
	hooks := config.Hooks
	if hooks.CreateTemp == nil {
		hooks.CreateTemp = os.CreateTemp
	}
	if hooks.WriteAll == nil {
		hooks.WriteAll = writeAll
	}
	if hooks.ReadFull == nil {
		hooks.ReadFull = io.ReadFull
	}
	if hooks.Close == nil {
		hooks.Close = func(file *os.File) error { return file.Close() }
	}
	directory, err := os.MkdirTemp(config.ParentDirectory, ".tammy-audit-openings-*")
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		_ = os.RemoveAll(directory)
		return nil, err
	}
	unsorted, err := hooks.CreateTemp(directory, "opening-digests.unsorted-*")
	if err != nil {
		_ = os.RemoveAll(directory)
		return nil, err
	}
	if err := unsorted.Chmod(0o600); err != nil {
		_ = unsorted.Close()
		_ = os.RemoveAll(directory)
		return nil, err
	}
	return &externalOpeningAccumulator{ctx: config.Context, directory: directory, unsorted: unsorted,
		recordLimit: config.RecordLimit, chunkRecords: config.ChunkRecords, mergeFanIn: config.MergeFanIn, hooks: hooks}, nil
}

func (accumulator *externalOpeningAccumulator) Add(
	sequence uint64,
	headBefore [sha256.Size]byte,
	openings *tammyv1.AuditCommitmentOpenings,
) error {
	if accumulator == nil {
		return ErrInvalidEvent
	}
	if accumulator.terminalErr != nil {
		return accumulator.terminalErr
	}
	if accumulator.closed || accumulator.finished || accumulator.unsorted == nil ||
		sequence == 0 || !validCommitmentOpenings(openings) {
		return ErrInvalidEvent
	}
	if err := accumulator.ctx.Err(); err != nil {
		return accumulator.abort(err)
	}
	values := commitmentOpeningBytes(openings)
	if uint64(len(values)) > accumulator.recordLimit-accumulator.recordCount {
		return accumulator.abort(ErrRepository)
	}
	for _, opening := range values {
		digest := sha256.New()
		_, _ = digest.Write([]byte(openingDigestDomain))
		_, _ = digest.Write(opening)
		record := externalOpeningRecord{sequence: sequence, headBefore: headBefore}
		copy(record.digest[:], digest.Sum(nil))
		var encoded [externalOpeningRecordSize]byte
		encodeExternalOpeningRecord(encoded[:], record)
		if err := accumulator.hooks.WriteAll(accumulator.unsorted, encoded[:]); err != nil {
			return accumulator.abort(err)
		}
		accumulator.recordCount++
	}
	return nil
}

func (accumulator *externalOpeningAccumulator) FirstDuplicate() (uint64, [sha256.Size]byte, error) {
	if accumulator == nil {
		return 0, [sha256.Size]byte{}, ErrRepository
	}
	if accumulator.terminalErr != nil {
		return 0, [sha256.Size]byte{}, accumulator.terminalErr
	}
	if accumulator.closed {
		return 0, [sha256.Size]byte{}, ErrRepository
	}
	if accumulator.finished {
		return accumulator.duplicateSequence, accumulator.headBeforeDuplicate, accumulator.terminalErr
	}
	if err := accumulator.ctx.Err(); err != nil {
		return 0, [sha256.Size]byte{}, accumulator.abort(err)
	}
	unsortedPath := accumulator.unsorted.Name()
	if err := accumulator.hooks.Close(accumulator.unsorted); err != nil {
		accumulator.unsorted = nil
		return 0, [sha256.Size]byte{}, accumulator.abort(err)
	}
	accumulator.unsorted = nil
	runs, duplicateSequence, duplicateHead, err := accumulator.createInitialRuns(unsortedPath)
	if err != nil {
		return 0, [sha256.Size]byte{}, accumulator.abort(err)
	}
	for len(runs) > 1 {
		next := make([]string, 0, (len(runs)+accumulator.mergeFanIn-1)/accumulator.mergeFanIn)
		for start := 0; start < len(runs); start += accumulator.mergeFanIn {
			if err := accumulator.ctx.Err(); err != nil {
				removeOpeningPaths(next)
				removeOpeningPaths(runs[start:])
				return 0, [sha256.Size]byte{}, accumulator.abort(err)
			}
			end := start + accumulator.mergeFanIn
			if end > len(runs) {
				end = len(runs)
			}
			if width := end - start; width > accumulator.maxMergeFanInObserved {
				accumulator.maxMergeFanInObserved = width
			}
			merged, foundSequence, foundHead, mergeErr := accumulator.mergeRuns(runs[start:end])
			if mergeErr != nil {
				removeOpeningPaths(next)
				removeOpeningPaths(runs[end:])
				return 0, [sha256.Size]byte{}, accumulator.abort(mergeErr)
			}
			duplicateSequence, duplicateHead = earlierDuplicate(
				duplicateSequence, duplicateHead, foundSequence, foundHead)
			next = append(next, merged)
		}
		runs = next
	}
	accumulator.finished = true
	accumulator.duplicateSequence = duplicateSequence
	accumulator.headBeforeDuplicate = duplicateHead
	return duplicateSequence, duplicateHead, nil
}

func (accumulator *externalOpeningAccumulator) createInitialRuns(
	path string,
) ([]string, uint64, [sha256.Size]byte, error) {
	input, err := os.Open(path)
	if err != nil {
		return nil, 0, [sha256.Size]byte{}, err
	}
	defer os.Remove(path)
	buffer := make([]byte, externalOpeningRecordSize*accumulator.chunkRecords)
	runCapacity := uint64(0)
	if accumulator.recordLimit != 0 {
		runCapacity = (accumulator.recordLimit + uint64(accumulator.chunkRecords) - 1) / uint64(accumulator.chunkRecords)
	}
	runs := make([]string, 0, int(runCapacity))
	var duplicateSequence uint64
	var duplicateHead [sha256.Size]byte
	for {
		if err := accumulator.ctx.Err(); err != nil {
			_ = input.Close()
			removeOpeningPaths(runs)
			return nil, 0, [sha256.Size]byte{}, err
		}
		n, readErr := accumulator.hooks.ReadFull(input, buffer)
		if readErr == io.EOF && n == 0 {
			break
		}
		if readErr != nil && readErr != io.ErrUnexpectedEOF {
			_ = input.Close()
			removeOpeningPaths(runs)
			return nil, 0, [sha256.Size]byte{}, readErr
		}
		if n%externalOpeningRecordSize != 0 {
			_ = input.Close()
			removeOpeningPaths(runs)
			return nil, 0, [sha256.Size]byte{}, ErrRepository
		}
		records := make([]externalOpeningRecord, n/externalOpeningRecordSize)
		if len(records) > accumulator.maxChunkRecordsObserved {
			accumulator.maxChunkRecordsObserved = len(records)
		}
		for index := range records {
			decodeExternalOpeningRecord(&records[index], buffer[index*externalOpeningRecordSize:(index+1)*externalOpeningRecordSize])
		}
		sort.Slice(records, func(left, right int) bool { return compareExternalOpeningRecords(records[left], records[right]) < 0 })
		for index := 1; index < len(records); index++ {
			if records[index-1].digest == records[index].digest {
				duplicateSequence, duplicateHead = earlierDuplicate(duplicateSequence, duplicateHead,
					records[index].sequence, records[index].headBefore)
			}
		}
		run, createErr := accumulator.writeRun(records)
		if createErr != nil {
			_ = input.Close()
			removeOpeningPaths(runs)
			return nil, 0, [sha256.Size]byte{}, createErr
		}
		runs = append(runs, run)
		if readErr == io.ErrUnexpectedEOF {
			break
		}
	}
	if err := accumulator.hooks.Close(input); err != nil {
		removeOpeningPaths(runs)
		return nil, 0, [sha256.Size]byte{}, err
	}
	return runs, duplicateSequence, duplicateHead, nil
}

func (accumulator *externalOpeningAccumulator) writeRun(records []externalOpeningRecord) (string, error) {
	file, err := accumulator.hooks.CreateTemp(accumulator.directory, "opening-digests.run-*")
	if err != nil {
		return "", err
	}
	path := file.Name()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", err
	}
	var encoded [externalOpeningRecordSize]byte
	for _, record := range records {
		encodeExternalOpeningRecord(encoded[:], record)
		if err := accumulator.hooks.WriteAll(file, encoded[:]); err != nil {
			_ = file.Close()
			_ = os.Remove(path)
			return "", err
		}
	}
	if err := accumulator.hooks.Close(file); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func (accumulator *externalOpeningAccumulator) mergeRuns(
	paths []string,
) (string, uint64, [sha256.Size]byte, error) {
	output, err := accumulator.hooks.CreateTemp(accumulator.directory, "opening-digests.merge-*")
	if err != nil {
		removeOpeningPaths(paths)
		return "", 0, [sha256.Size]byte{}, err
	}
	outputPath := output.Name()
	if err := output.Chmod(0o600); err != nil {
		_ = output.Close()
		_ = os.Remove(outputPath)
		removeOpeningPaths(paths)
		return "", 0, [sha256.Size]byte{}, err
	}
	readers := make([]*externalOpeningRunReader, 0, len(paths))
	queue := externalOpeningRunHeap{}
	cleanup := func() error {
		var closeErr error
		for _, reader := range readers {
			if err := accumulator.hooks.Close(reader.file); err != nil && closeErr == nil {
				closeErr = err
			}
		}
		removeOpeningPaths(paths)
		return closeErr
	}
	for _, path := range paths {
		file, openErr := os.Open(path)
		if openErr != nil {
			_ = cleanup()
			_ = output.Close()
			_ = os.Remove(outputPath)
			return "", 0, [sha256.Size]byte{}, openErr
		}
		reader := &externalOpeningRunReader{file: file, readFull: accumulator.hooks.ReadFull}
		readers = append(readers, reader)
		if readErr := reader.advance(); readErr != nil && readErr != io.EOF {
			_ = cleanup()
			_ = output.Close()
			_ = os.Remove(outputPath)
			return "", 0, [sha256.Size]byte{}, readErr
		} else if readErr == nil {
			heap.Push(&queue, reader)
		}
	}
	var previous externalOpeningRecord
	havePrevious := false
	var duplicateSequence uint64
	var duplicateHead [sha256.Size]byte
	var encoded [externalOpeningRecordSize]byte
	for queue.Len() != 0 {
		if err := accumulator.ctx.Err(); err != nil {
			_ = cleanup()
			_ = output.Close()
			_ = os.Remove(outputPath)
			return "", 0, [sha256.Size]byte{}, err
		}
		reader := heap.Pop(&queue).(*externalOpeningRunReader)
		record := reader.current
		if havePrevious && previous.digest == record.digest {
			duplicateSequence, duplicateHead = earlierDuplicate(duplicateSequence, duplicateHead,
				record.sequence, record.headBefore)
		}
		encodeExternalOpeningRecord(encoded[:], record)
		if err := accumulator.hooks.WriteAll(output, encoded[:]); err != nil {
			_ = cleanup()
			_ = output.Close()
			_ = os.Remove(outputPath)
			return "", 0, [sha256.Size]byte{}, err
		}
		previous, havePrevious = record, true
		if readErr := reader.advance(); readErr != nil && readErr != io.EOF {
			_ = cleanup()
			_ = output.Close()
			_ = os.Remove(outputPath)
			return "", 0, [sha256.Size]byte{}, readErr
		} else if readErr == nil {
			heap.Push(&queue, reader)
		}
	}
	if err := cleanup(); err != nil {
		_ = output.Close()
		_ = os.Remove(outputPath)
		return "", 0, [sha256.Size]byte{}, err
	}
	if err := accumulator.hooks.Close(output); err != nil {
		_ = os.Remove(outputPath)
		return "", 0, [sha256.Size]byte{}, err
	}
	return outputPath, duplicateSequence, duplicateHead, nil
}

func (accumulator *externalOpeningAccumulator) abort(err error) error {
	if accumulator == nil {
		return err
	}
	if accumulator.terminalErr != nil {
		return accumulator.terminalErr
	}
	accumulator.terminalErr = err
	if accumulator.unsorted != nil {
		_ = accumulator.unsorted.Close()
		accumulator.unsorted = nil
	}
	_ = os.RemoveAll(accumulator.directory)
	return err
}

func (accumulator *externalOpeningAccumulator) Close() error {
	if accumulator == nil || accumulator.closed {
		return nil
	}
	accumulator.closed = true
	var closeErr error
	if accumulator.unsorted != nil {
		closeErr = accumulator.hooks.Close(accumulator.unsorted)
		accumulator.unsorted = nil
	}
	if err := os.RemoveAll(accumulator.directory); closeErr == nil {
		closeErr = err
	}
	return closeErr
}

type externalOpeningRunReader struct {
	file     *os.File
	readFull func(io.Reader, []byte) (int, error)
	current  externalOpeningRecord
}

func (reader *externalOpeningRunReader) advance() error {
	var encoded [externalOpeningRecordSize]byte
	if _, err := reader.readFull(reader.file, encoded[:]); err != nil {
		return err
	}
	decodeExternalOpeningRecord(&reader.current, encoded[:])
	return nil
}

type externalOpeningRunHeap []*externalOpeningRunReader

func (items externalOpeningRunHeap) Len() int { return len(items) }
func (items externalOpeningRunHeap) Less(left, right int) bool {
	return compareExternalOpeningRecords(items[left].current, items[right].current) < 0
}
func (items externalOpeningRunHeap) Swap(left, right int) {
	items[left], items[right] = items[right], items[left]
}
func (items *externalOpeningRunHeap) Push(value any) {
	*items = append(*items, value.(*externalOpeningRunReader))
}
func (items *externalOpeningRunHeap) Pop() any {
	old := *items
	value := old[len(old)-1]
	*items = old[:len(old)-1]
	return value
}

func encodeExternalOpeningRecord(destination []byte, record externalOpeningRecord) {
	copy(destination[:sha256.Size], record.digest[:])
	binary.BigEndian.PutUint64(destination[sha256.Size:sha256.Size+8], record.sequence)
	copy(destination[sha256.Size+8:], record.headBefore[:])
}

func decodeExternalOpeningRecord(record *externalOpeningRecord, encoded []byte) {
	copy(record.digest[:], encoded[:sha256.Size])
	record.sequence = binary.BigEndian.Uint64(encoded[sha256.Size : sha256.Size+8])
	copy(record.headBefore[:], encoded[sha256.Size+8:])
}

func compareExternalOpeningRecords(left, right externalOpeningRecord) int {
	if compared := bytes.Compare(left.digest[:], right.digest[:]); compared != 0 {
		return compared
	}
	if left.sequence < right.sequence {
		return -1
	}
	if left.sequence > right.sequence {
		return 1
	}
	return bytes.Compare(left.headBefore[:], right.headBefore[:])
}

func earlierDuplicate(
	currentSequence uint64,
	currentHead [sha256.Size]byte,
	candidateSequence uint64,
	candidateHead [sha256.Size]byte,
) (uint64, [sha256.Size]byte) {
	if candidateSequence != 0 && (currentSequence == 0 || candidateSequence < currentSequence) {
		return candidateSequence, candidateHead
	}
	return currentSequence, currentHead
}

func removeOpeningPaths(paths []string) {
	for _, path := range paths {
		_ = os.Remove(path)
	}
}

func writeAll(writer io.Writer, value []byte) error {
	for len(value) != 0 {
		written, err := writer.Write(value)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		value = value[written:]
	}
	return nil
}

var _ commitmentOpeningAccumulator = (*externalOpeningAccumulator)(nil)
