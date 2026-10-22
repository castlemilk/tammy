package identity

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"sort"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
)

func loadRepositoryStateFrom(ctx context.Context, executor workspace.MutationExecutor) (repositoryState, error) {
	if executor == nil {
		return repositoryState{}, ErrRepositoryIntegrity
	}
	state := newRepositoryState()
	loaders := []func(context.Context, workspace.MutationExecutor, *repositoryState) error{
		loadUsers, loadUserRoles, loadPasswordHistory, loadApplicationSessions,
		loadTOTPFactors, loadFactorAssertions, loadCommandIdempotency,
	}
	for _, load := range loaders {
		if err := load(ctx, executor, &state); err != nil {
			return repositoryState{}, ErrRepositoryIntegrity
		}
	}
	if err := validateRepositoryState(state); err != nil {
		return repositoryState{}, ErrRepositoryIntegrity
	}
	return state, nil
}

func loadUsers(ctx context.Context, executor workspace.MutationExecutor, state *repositoryState) error {
	rows, err := executor.QueryContext(ctx, `SELECT id, email, normalized_username, display_name, status, version,
		password_policy_version, password_memory_kib, password_iterations, password_parallelism,
		password_salt, password_digest, activation_hash, activation_consumed_hash, activation_encrypted,
		activation_expires_at, activation_fails, activation_session_id, sign_in_failure_times,
		failed_attempts, locked_until, repository_version
		FROM users ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			record                                               userRecord
			normalized, status, activationExpires, activationSID sql.NullString
			lockedUntil                                          sql.NullString
			policy, memory, iterations, parallelism              sql.NullInt64
			salt, digest, failureTimes                           []byte
			failedAttempts                                       int
			repositoryVersion                                    uint64
		)
		if err := rows.Scan(&record.ID, &record.Username, &normalized, &record.DisplayName, &status,
			&record.Version, &policy, &memory, &iterations, &parallelism, &salt, &digest,
			&record.ActivationHash, &record.ActivationConsumedHash, &record.ActivationEncrypted,
			&activationExpires, &record.ActivationFails, &activationSID, &failureTimes,
			&failedAttempts, &lockedUntil, &repositoryVersion); err != nil {
			return err
		}
		if record.ID == "" || record.Username == "" || record.DisplayName == "" || record.Version == 0 ||
			repositoryVersion == 0 || !normalized.Valid || normalized.String != normalizedUsername(record.Username) {
			return ErrRepositoryIntegrity
		}
		record.State, err = parseUserState(status.String)
		if err != nil {
			return err
		}
		record.Password, err = parseVerifier(policy, memory, iterations, parallelism, salt, digest)
		if err != nil {
			return err
		}
		record.ActivationExpires, err = parseOptionalTime(activationExpires)
		if err != nil {
			return err
		}
		if activationSID.Valid {
			record.ActivationSessionID = activationSID.String
		}
		record.SignInFailures, err = decodeFailureTimes(failureTimes)
		if err != nil || failedAttempts != len(record.SignInFailures) {
			return ErrRepositoryIntegrity
		}
		record.LockedUntil, err = parseOptionalTime(lockedUntil)
		if err != nil {
			return err
		}
		if _, duplicate := state.Users[record.ID]; duplicate {
			return ErrRepositoryIntegrity
		}
		state.Users[record.ID] = &record
		state.Usernames[normalized.String] = record.ID
		state.persistence.users[record.ID] = repositoryVersion
	}
	return rows.Err()
}

func loadUserRoles(ctx context.Context, executor workspace.MutationExecutor, state *repositoryState) error {
	rows, err := executor.QueryContext(ctx, `SELECT user_id, role_code, repository_version FROM user_roles ORDER BY user_id, role_code`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var userID, code string
		var repositoryVersion uint64
		if err := rows.Scan(&userID, &code, &repositoryVersion); err != nil {
			return err
		}
		user := state.Users[userID]
		role, ok := roleFromCode(code)
		key := userRoleRowKey{userID: userID, roleCode: code}
		if user == nil || !ok || repositoryVersion == 0 || state.persistence.userRoles[key] != 0 {
			return ErrRepositoryIntegrity
		}
		user.Roles = append(user.Roles, role)
		state.persistence.userRoles[key] = repositoryVersion
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, user := range state.Users {
		if len(user.Roles) == 0 {
			return ErrRepositoryIntegrity
		}
		sort.Slice(user.Roles, func(left, right int) bool { return user.Roles[left] < user.Roles[right] })
	}
	return nil
}

func loadPasswordHistory(ctx context.Context, executor workspace.MutationExecutor, state *repositoryState) error {
	rows, err := executor.QueryContext(ctx, `SELECT user_id, ordinal, policy_version, memory_kib, iterations,
		parallelism, salt, digest, repository_version FROM user_password_history ORDER BY user_id, ordinal`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var userID string
		var ordinal int
		var policy, memory, iterations, parallelism int64
		var salt, digest []byte
		var repositoryVersion uint64
		if err := rows.Scan(&userID, &ordinal, &policy, &memory, &iterations, &parallelism,
			&salt, &digest, &repositoryVersion); err != nil {
			return err
		}
		user := state.Users[userID]
		key := passwordHistoryRowKey{userID: userID, ordinal: ordinal}
		verifier, err := parseVerifier(
			sql.NullInt64{Int64: policy, Valid: true}, sql.NullInt64{Int64: memory, Valid: true},
			sql.NullInt64{Int64: iterations, Valid: true}, sql.NullInt64{Int64: parallelism, Valid: true},
			salt, digest,
		)
		if err != nil || user == nil || ordinal != len(user.PasswordHistory) || repositoryVersion == 0 ||
			state.persistence.passwordHistory[key] != 0 {
			return ErrRepositoryIntegrity
		}
		user.PasswordHistory = append(user.PasswordHistory, verifier)
		state.persistence.passwordHistory[key] = repositoryVersion
	}
	return rows.Err()
}

func loadApplicationSessions(ctx context.Context, executor workspace.MutationExecutor, state *repositoryState) error {
	rows, err := executor.QueryContext(ctx, `SELECT id, user_id, state, created_at, last_active_at, expires_at,
		ended_at, repository_version FROM application_sessions ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var record sessionRecord
		var sessionState int32
		var createdAt, lastActive, expiresAt string
		var endedAt sql.NullString
		var repositoryVersion uint64
		if err := rows.Scan(&record.ID, &record.UserID, &sessionState, &createdAt, &lastActive,
			&expiresAt, &endedAt, &repositoryVersion); err != nil {
			return err
		}
		var err error
		record.State = tammyv1.SessionState(sessionState)
		record.CreatedAt, err = parseRequiredTime(createdAt)
		if err == nil {
			record.LastActive, err = parseRequiredTime(lastActive)
		}
		if err == nil {
			record.ExpiresAt, err = parseRequiredTime(expiresAt)
		}
		if err == nil {
			record.EndedAt, err = parseOptionalTime(endedAt)
		}
		if err != nil || record.ID == "" || state.Users[record.UserID] == nil ||
			record.State < tammyv1.SessionState_SESSION_STATE_ACTIVE ||
			record.State > tammyv1.SessionState_SESSION_STATE_INVALIDATED || repositoryVersion == 0 {
			return ErrRepositoryIntegrity
		}
		state.Sessions[record.ID] = &record
		state.persistence.sessions[record.ID] = repositoryVersion
	}
	return rows.Err()
}

func loadTOTPFactors(ctx context.Context, executor workspace.MutationExecutor, state *repositoryState) error {
	rows, err := executor.QueryContext(ctx, `SELECT id, user_id, version, state, created_at, encrypted_secret,
		last_counter, repository_version FROM totp_factors ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var record factorRecord
		var factorState int32
		var createdAt string
		var repositoryVersion uint64
		if err := rows.Scan(&record.ID, &record.UserID, &record.Version, &factorState, &createdAt,
			&record.EncryptedSecret, &record.LastCounter, &repositoryVersion); err != nil {
			return err
		}
		var err error
		record.CreatedAt, err = parseRequiredTime(createdAt)
		record.State = tammyv1.FactorState(factorState)
		if err != nil || record.ID == "" || record.Version == 0 || state.Users[record.UserID] == nil ||
			record.State < tammyv1.FactorState_FACTOR_STATE_PENDING_CONFIRMATION ||
			record.State > tammyv1.FactorState_FACTOR_STATE_DISABLED || repositoryVersion == 0 {
			return ErrRepositoryIntegrity
		}
		state.Factors[record.ID] = &record
		state.persistence.factors[record.ID] = repositoryVersion
	}
	return rows.Err()
}

func loadFactorAssertions(ctx context.Context, executor workspace.MutationExecutor, state *repositoryState) error {
	rows, err := executor.QueryContext(ctx, `SELECT id, user_id, session_id, purpose, asserted_at, consumed,
		repository_version FROM factor_assertions ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var record assertionRecord
		var assertedAt string
		var consumed int
		var repositoryVersion uint64
		if err := rows.Scan(&record.ID, &record.UserID, &record.SessionID, &record.Purpose,
			&assertedAt, &consumed, &repositoryVersion); err != nil {
			return err
		}
		var err error
		record.Asserted, err = parseRequiredTime(assertedAt)
		if err != nil || record.ID == "" || record.Purpose == "" || state.Users[record.UserID] == nil ||
			state.Sessions[record.SessionID] == nil || (consumed != 0 && consumed != 1) || repositoryVersion == 0 {
			return ErrRepositoryIntegrity
		}
		record.Consumed = consumed == 1
		state.Assertions[record.ID] = &record
		state.persistence.assertions[record.ID] = repositoryVersion
	}
	return rows.Err()
}

func loadCommandIdempotency(ctx context.Context, executor workspace.MutationExecutor, state *repositoryState) error {
	rows, err := executor.QueryContext(ctx, `SELECT operation_key, command_type, semantic_sha256, actor_user_id,
		user_id, factor_id, session_id, response_encrypted, repository_version, created_at
		FROM command_idempotency ORDER BY operation_key`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var key, createdAt string
		var record idempotencyRecord
		var actorID, userID, factorID, sessionID sql.NullString
		var repositoryVersion uint64
		if err := rows.Scan(&key, &record.Command, &record.SemanticHash, &actorID, &userID,
			&factorID, &sessionID, &record.ResponseEncrypted, &repositoryVersion, &createdAt); err != nil {
			return err
		}
		if _, err := parseRequiredTime(createdAt); err != nil || key == "" || record.Command == "" ||
			len(record.SemanticHash) != sha256.Size*2 || repositoryVersion == 0 {
			return ErrRepositoryIntegrity
		}
		if actorID.Valid {
			record.ActorUserID = actorID.String
		}
		if userID.Valid {
			record.UserID = userID.String
		}
		if factorID.Valid {
			record.FactorID = factorID.String
		}
		if sessionID.Valid {
			record.SessionID = sessionID.String
		}
		state.Idempotency[key] = record
		state.persistence.idempotency[key] = repositoryVersion
		state.persistence.idempotencyDigests[key] = idempotencyDigest(record)
	}
	return rows.Err()
}

func parseUserState(value string) (tammyv1.UserState, error) {
	switch value {
	case "PENDING":
		return tammyv1.UserState_USER_STATE_PENDING_ACTIVATION, nil
	case "ACTIVE":
		return tammyv1.UserState_USER_STATE_ACTIVE, nil
	case "LOCKED":
		return tammyv1.UserState_USER_STATE_AUTHENTICATION_LOCKED, nil
	default:
		return tammyv1.UserState_USER_STATE_UNSPECIFIED, ErrRepositoryIntegrity
	}
}

func userStateCode(value tammyv1.UserState) (string, error) {
	switch value {
	case tammyv1.UserState_USER_STATE_PENDING_ACTIVATION:
		return "PENDING", nil
	case tammyv1.UserState_USER_STATE_ACTIVE:
		return "ACTIVE", nil
	case tammyv1.UserState_USER_STATE_AUTHENTICATION_LOCKED:
		return "LOCKED", nil
	default:
		return "", ErrRepositoryIntegrity
	}
}

func roleFromCode(code string) (tammyv1.Role, bool) {
	switch code {
	case "workspace_admin":
		return tammyv1.Role_ROLE_WORKSPACE_ADMIN, true
	case "business_preparer":
		return tammyv1.Role_ROLE_BUSINESS_PREPARER, true
	case "business_lodger":
		return tammyv1.Role_ROLE_BUSINESS_LODGER, true
	case "auditor":
		return tammyv1.Role_ROLE_AUDITOR, true
	default:
		return tammyv1.Role_ROLE_UNSPECIFIED, false
	}
}

func roleCode(role tammyv1.Role) (string, error) {
	switch role {
	case tammyv1.Role_ROLE_WORKSPACE_ADMIN:
		return "workspace_admin", nil
	case tammyv1.Role_ROLE_BUSINESS_PREPARER:
		return "business_preparer", nil
	case tammyv1.Role_ROLE_BUSINESS_LODGER:
		return "business_lodger", nil
	case tammyv1.Role_ROLE_AUDITOR:
		return "auditor", nil
	default:
		return "", ErrRepositoryIntegrity
	}
}

func parseVerifier(policy, memory, iterations, parallelism sql.NullInt64, salt, digest []byte) (workspace.PasswordVerifier, error) {
	allMissing := !policy.Valid && !memory.Valid && !iterations.Valid && !parallelism.Valid && salt == nil && digest == nil
	if allMissing {
		return workspace.PasswordVerifier{}, nil
	}
	if !policy.Valid || !memory.Valid || !iterations.Valid || !parallelism.Valid ||
		policy.Int64 != 1 || memory.Int64 != 64*1024 || iterations.Int64 != 3 || parallelism.Int64 != 1 ||
		len(salt) != 16 || len(digest) != sha256.Size {
		return workspace.PasswordVerifier{}, ErrRepositoryIntegrity
	}
	return workspace.PasswordVerifier{
		PolicyVersion: uint16(policy.Int64), MemoryKiB: uint32(memory.Int64), Iterations: uint32(iterations.Int64),
		Parallelism: uint8(parallelism.Int64), Salt: append([]byte(nil), salt...), Digest: append([]byte(nil), digest...),
	}, nil
}

func parseRequiredTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.Location() != time.UTC || parsed.Format(time.RFC3339Nano) != value {
		return time.Time{}, ErrRepositoryIntegrity
	}
	return parsed, nil
}

func parseOptionalTime(value sql.NullString) (time.Time, error) {
	if !value.Valid {
		return time.Time{}, nil
	}
	return parseRequiredTime(value.String)
}

func decodeFailureTimes(encoded []byte) ([]time.Time, error) {
	if len(encoded)%8 != 0 || len(encoded) > 40 {
		return nil, ErrRepositoryIntegrity
	}
	decoded := make([]time.Time, len(encoded)/8)
	for index := range decoded {
		decoded[index] = time.Unix(0, int64(binary.BigEndian.Uint64(encoded[index*8:]))).UTC()
	}
	return decoded, nil
}

func idempotencyDigest(record idempotencyRecord) [32]byte {
	digest := sha256.New()
	var length [8]byte
	write := func(value []byte) {
		binary.BigEndian.PutUint64(length[:], uint64(len(value)))
		_, _ = digest.Write(length[:])
		_, _ = digest.Write(value)
	}
	for _, value := range []string{record.Command, record.SemanticHash, record.ActorUserID, record.UserID, record.FactorID, record.SessionID} {
		write([]byte(value))
	}
	write(record.ResponseEncrypted)
	var result [32]byte
	copy(result[:], digest.Sum(nil))
	return result
}
