//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package sqlcipher

/*
#include <sqlite3.h>
#include <stdlib.h>

int sqlite3_key(sqlite3*, const void*, int);
int tammy_sqlite3_main_file_matches(sqlite3*, sqlite3_uint64);

static void tammy_backup_zero(void *value, int length) {
  if(value != NULL && length > 0) {
    volatile unsigned char *bytes = (volatile unsigned char *)value;
    while(length-- > 0) *bytes++ = 0;
  }
}

typedef struct tammy_online_backup_handle {
  sqlite3 *destination;
  sqlite3_backup *backup;
} tammy_online_backup_handle;

// The destination must already exist. Omitting SQLITE_OPEN_CREATE ensures a
// swapped pathname cannot create or write a second file outside the retained
// caller-owned directory handle. Identity is checked before backup_init.
static tammy_online_backup_handle *tammy_online_backup_start(
    sqlite3 *source,
    const char *destination_path,
    sqlite3_uint64 expected_file_descriptor,
    const void *key,
    int key_length,
    int *result_out) {
  *result_out = SQLITE_ERROR;
  tammy_online_backup_handle *handle = calloc(1, sizeof(tammy_online_backup_handle));
  if(handle == NULL) return NULL;
  sqlite3 *destination = NULL;
  int flags = SQLITE_OPEN_READWRITE | SQLITE_OPEN_FULLMUTEX | SQLITE_OPEN_NOFOLLOW;
  int result = sqlite3_open_v2(destination_path, &destination, flags, NULL);
  if(result != SQLITE_OK) {
    if(destination != NULL) sqlite3_close_v2(destination);
    free(handle);
    *result_out = result;
    return NULL;
  }
  result = sqlite3_key(destination, key, key_length);
  if(result != SQLITE_OK || tammy_sqlite3_main_file_matches(destination, expected_file_descriptor) != 1) {
    sqlite3_close_v2(destination);
    free(handle);
    *result_out = result == SQLITE_OK ? SQLITE_MISUSE : result;
    return NULL;
  }
  sqlite3_backup *backup = sqlite3_backup_init(destination, "main", source, "main");
  if(backup == NULL) {
    result = sqlite3_errcode(destination);
    sqlite3_close_v2(destination);
    free(handle);
    *result_out = result;
    return NULL;
  }
  handle->destination = destination;
  handle->backup = backup;
  *result_out = SQLITE_OK;
  return handle;
}

static int tammy_online_backup_step(tammy_online_backup_handle *handle, int pages) {
  if(handle == NULL || handle->backup == NULL || pages <= 0) return SQLITE_MISUSE;
  return sqlite3_backup_step(handle->backup, pages);
}

static int tammy_online_backup_remaining(tammy_online_backup_handle *handle) {
  if(handle == NULL || handle->backup == NULL) return -1;
  return sqlite3_backup_remaining(handle->backup);
}

static int tammy_online_backup_pagecount(tammy_online_backup_handle *handle) {
  if(handle == NULL || handle->backup == NULL) return -1;
  return sqlite3_backup_pagecount(handle->backup);
}

static int tammy_online_backup_finish(tammy_online_backup_handle *handle) {
  if(handle == NULL) return SQLITE_MISUSE;
  int result = SQLITE_OK;
  if(handle->backup != NULL) result = sqlite3_backup_finish(handle->backup);
  if(handle->destination != NULL) {
    int close_result = sqlite3_close_v2(handle->destination);
    if(result == SQLITE_OK && close_result != SQLITE_OK) result = close_result;
  }
  free(handle);
  return result;
}
*/
import "C"

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"unsafe"
)

var (
	ErrOnlineBackup      = errors.New("sqlcipher: online backup failed")
	errOnlineBackupStart = errors.New("sqlcipher: online backup start failed")
)

const onlineBackupPagesPerStep = 128

type onlineBackupHooks struct {
	afterStep func(remaining, total int) error
}

// OnlineBackupTo copies one consistent source snapshot into an already-open,
// empty, restrictive destination file. It never creates the destination path.
func (database *Database) OnlineBackupTo(ctx context.Context, destinationPath string, expected *os.File, key []byte) error {
	return database.onlineBackupTo(ctx, destinationPath, expected, key, onlineBackupHooks{})
}

// OnlineBackupToWithProgress is OnlineBackupTo with a bounded callback after
// each page step. Returning an error stops before the next step.
func (database *Database) OnlineBackupToWithProgress(
	ctx context.Context,
	destinationPath string,
	expected *os.File,
	key []byte,
	progress func(remaining, total int) error,
) error {
	if progress == nil {
		return ErrOnlineBackup
	}
	return database.onlineBackupTo(ctx, destinationPath, expected, key, onlineBackupHooks{afterStep: progress})
}

func (database *Database) onlineBackupTo(
	ctx context.Context,
	destinationPath string,
	expected *os.File,
	key []byte,
	hooks onlineBackupHooks,
) error {
	if database == nil || database.DB == nil || database.DB != database.authenticatedDB || database.connector == nil ||
		ctx == nil || destinationPath == "" || !filepath.IsAbs(destinationPath) || filepath.Clean(destinationPath) != destinationPath ||
		expected == nil || len(key) != KeySize {
		return ErrOnlineBackup
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrOnlineBackup, err)
	}
	cleanedDestinationPath, err := validateDatabasePath(destinationPath)
	if err != nil {
		return ErrOnlineBackup
	}
	expectedInfo, err := expected.Stat()
	if err != nil || !expectedInfo.Mode().IsRegular() || expectedInfo.Mode().Perm() != 0o600 || expectedInfo.Size() != 0 {
		return ErrOnlineBackup
	}
	pathInfo, err := os.Lstat(cleanedDestinationPath)
	if err != nil || !os.SameFile(expectedInfo, pathInfo) {
		return ErrOnlineBackup
	}
	connection, err := database.Conn(ctx)
	if err != nil {
		return ErrOnlineBackup
	}
	defer connection.Close()
	err = connection.Raw(func(driverConnection any) error {
		source, ok := driverConnection.(*sqliteConn)
		if !ok || source == nil || source.database == nil {
			return ErrOnlineBackup
		}
		source.mu.Lock()
		defer source.mu.Unlock()
		pathValue := C.CString(cleanedDestinationPath)
		if pathValue == nil {
			return ErrOnlineBackup
		}
		defer C.free(unsafe.Pointer(pathValue))
		keyValue := C.CBytes(key)
		if keyValue == nil {
			return ErrOnlineBackup
		}
		defer func() {
			C.tammy_backup_zero(keyValue, C.int(len(key)))
			C.free(keyValue)
		}()
		var startResult C.int
		handle := C.tammy_online_backup_start(source.database, pathValue, C.sqlite3_uint64(expected.Fd()), keyValue,
			C.int(len(key)), &startResult)
		if handle == nil || startResult != C.SQLITE_OK {
			return errors.Join(ErrOnlineBackup, errOnlineBackupStart)
		}
		finished := false
		defer func() {
			if !finished {
				_ = C.tammy_online_backup_finish(handle)
			}
		}()
		for attempts := 0; attempts < 100_000; attempts++ {
			if err := ctx.Err(); err != nil {
				return errors.Join(ErrOnlineBackup, err)
			}
			result := C.tammy_online_backup_step(handle, onlineBackupPagesPerStep)
			if hooks.afterStep != nil {
				remaining := int(C.tammy_online_backup_remaining(handle))
				total := int(C.tammy_online_backup_pagecount(handle))
				if remaining < 0 || total <= 0 || remaining > total {
					return ErrOnlineBackup
				}
				if err := hooks.afterStep(remaining, total); err != nil {
					return errors.Join(ErrOnlineBackup, err)
				}
			}
			switch result {
			case C.SQLITE_DONE:
				finishResult := C.tammy_online_backup_finish(handle)
				finished = true
				if finishResult != C.SQLITE_OK {
					return ErrOnlineBackup
				}
				return nil
			case C.SQLITE_OK, C.SQLITE_BUSY, C.SQLITE_LOCKED:
				continue
			default:
				return ErrOnlineBackup
			}
		}
		return ErrOnlineBackup
	})
	if contextErr := ctx.Err(); contextErr != nil {
		return errors.Join(ErrOnlineBackup, contextErr)
	}
	if err != nil {
		return errors.Join(ErrOnlineBackup, err)
	}
	finalInfo, err := expected.Stat()
	if err != nil || !os.SameFile(expectedInfo, finalInfo) || finalInfo.Size() <= 0 {
		return ErrOnlineBackup
	}
	current, err := os.Lstat(cleanedDestinationPath)
	if err != nil || !os.SameFile(expectedInfo, current) {
		return ErrOnlineBackup
	}
	return nil
}
