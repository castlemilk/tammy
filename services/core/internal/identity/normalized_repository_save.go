package identity

import (
	"context"
	"encoding/binary"
	"sort"
	"strings"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
)

func saveRepositoryStateTo(ctx context.Context, executor workspace.MutationExecutor, state repositoryState) error {
	if executor == nil {
		return ErrRepositoryIntegrity
	}
	normalizeRepositoryState(&state)
	if err := validateRepositoryState(state); err != nil {
		return err
	}
	for key, digest := range state.persistence.idempotencyDigests {
		record, retained := state.Idempotency[key]
		if !retained || idempotencyDigest(record) != digest {
			return ErrRepositoryIntegrity
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := deleteMissingIdentityRows(ctx, executor, state); err != nil {
		return err
	}
	if err := saveUsers(ctx, executor, state, now); err != nil {
		return err
	}
	if err := saveUserRoles(ctx, executor, state, now); err != nil {
		return err
	}
	if err := savePasswordHistory(ctx, executor, state); err != nil {
		return err
	}
	if err := saveApplicationSessions(ctx, executor, state); err != nil {
		return err
	}
	if err := saveTOTPFactors(ctx, executor, state); err != nil {
		return err
	}
	if err := saveFactorAssertions(ctx, executor, state); err != nil {
		return err
	}
	return saveCommandIdempotency(ctx, executor, state, now)
}

func validateRepositoryState(state repositoryState) error {
	if len(state.Usernames) != len(state.Users) {
		return ErrRepositoryIntegrity
	}
	activeSessions := 0
	activeFactors := make(map[string]bool)
	for id, user := range state.Users {
		if user == nil || id == "" || user.ID != id || user.Version == 0 || user.Username == "" || user.DisplayName == "" ||
			state.Usernames[normalizedUsername(user.Username)] != id || len(user.SignInFailures) > 5 || user.ActivationFails < 0 {
			return ErrRepositoryIntegrity
		}
		if _, err := userStateCode(user.State); err != nil {
			return err
		}
		if _, _, _, _, _, _, err := verifierDatabaseValues(user.Password); err != nil {
			return err
		}
		if user.State != tammyv1.UserState_USER_STATE_PENDING_ACTIVATION && user.Password.PolicyVersion == 0 {
			return ErrRepositoryIntegrity
		}
		if len(user.ActivationHash) != 0 && len(user.ActivationHash) != 32 ||
			len(user.ActivationConsumedHash) != 0 && len(user.ActivationConsumedHash) != 32 {
			return ErrRepositoryIntegrity
		}
		seenRoles := make(map[tammyv1.Role]bool, len(user.Roles))
		if len(user.Roles) == 0 || len(user.Roles) > 4 {
			return ErrRepositoryIntegrity
		}
		for _, role := range user.Roles {
			if _, err := roleCode(role); err != nil || seenRoles[role] {
				return ErrRepositoryIntegrity
			}
			seenRoles[role] = true
		}
		if len(user.PasswordHistory) > 5 {
			return ErrRepositoryIntegrity
		}
		for _, verifier := range user.PasswordHistory {
			if _, _, _, _, _, _, err := verifierDatabaseValues(verifier); err != nil || verifier.PolicyVersion == 0 {
				return ErrRepositoryIntegrity
			}
		}
	}
	for key, userID := range state.Usernames {
		user := state.Users[userID]
		if user == nil || key != normalizedUsername(user.Username) {
			return ErrRepositoryIntegrity
		}
	}
	for id, session := range state.Sessions {
		if session == nil || id == "" || session.ID != id || state.Users[session.UserID] == nil ||
			session.CreatedAt.IsZero() || session.LastActive.IsZero() || session.ExpiresAt.IsZero() ||
			session.State < tammyv1.SessionState_SESSION_STATE_ACTIVE ||
			session.State > tammyv1.SessionState_SESSION_STATE_INVALIDATED {
			return ErrRepositoryIntegrity
		}
		if session.State == tammyv1.SessionState_SESSION_STATE_ACTIVE {
			activeSessions++
			if !session.EndedAt.IsZero() {
				return ErrRepositoryIntegrity
			}
		} else if session.EndedAt.IsZero() {
			return ErrRepositoryIntegrity
		}
	}
	if activeSessions > 1 {
		return ErrRepositoryIntegrity
	}
	for id, factor := range state.Factors {
		if factor == nil || id == "" || factor.ID != id || factor.Version == 0 || state.Users[factor.UserID] == nil ||
			factor.CreatedAt.IsZero() || factor.LastCounter < -1 ||
			factor.State < tammyv1.FactorState_FACTOR_STATE_PENDING_CONFIRMATION ||
			factor.State > tammyv1.FactorState_FACTOR_STATE_DISABLED {
			return ErrRepositoryIntegrity
		}
		if factor.State == tammyv1.FactorState_FACTOR_STATE_DISABLED {
			if factor.EncryptedSecret != nil {
				return ErrRepositoryIntegrity
			}
		} else {
			if len(factor.EncryptedSecret) == 0 || activeFactors[factor.UserID] {
				return ErrRepositoryIntegrity
			}
			activeFactors[factor.UserID] = true
		}
	}
	for id, assertion := range state.Assertions {
		if assertion == nil || id == "" || assertion.ID != id || state.Users[assertion.UserID] == nil ||
			state.Sessions[assertion.SessionID] == nil || assertion.Purpose == "" || len(assertion.Purpose) > 128 ||
			assertion.Asserted.IsZero() {
			return ErrRepositoryIntegrity
		}
	}
	for key, record := range state.Idempotency {
		if key == "" || record.Command == "" || len(record.Command) > 128 || len(record.SemanticHash) != 64 ||
			record.ActorUserID != "" && state.Users[record.ActorUserID] == nil ||
			record.UserID != "" && state.Users[record.UserID] == nil ||
			record.FactorID != "" && state.Factors[record.FactorID] == nil ||
			record.SessionID != "" && state.Sessions[record.SessionID] == nil {
			return ErrRepositoryIntegrity
		}
	}
	return nil
}

func deleteMissingIdentityRows(ctx context.Context, executor workspace.MutationExecutor, state repositoryState) error {
	if err := deleteMissingStringRows(ctx, executor, "factor_assertions", "id", state.persistence.assertions, func(key string) bool {
		_, present := state.Assertions[key]
		return present
	}); err != nil {
		return err
	}
	if err := deleteMissingStringRows(ctx, executor, "totp_factors", "id", state.persistence.factors, func(key string) bool {
		_, present := state.Factors[key]
		return present
	}); err != nil {
		return err
	}
	if err := deleteMissingStringRows(ctx, executor, "application_sessions", "id", state.persistence.sessions, func(key string) bool {
		_, present := state.Sessions[key]
		return present
	}); err != nil {
		return err
	}
	historyKeys := make([]passwordHistoryRowKey, 0, len(state.persistence.passwordHistory))
	for key := range state.persistence.passwordHistory {
		if user := state.Users[key.userID]; user == nil || key.ordinal >= len(user.PasswordHistory) {
			historyKeys = append(historyKeys, key)
		}
	}
	sort.Slice(historyKeys, func(i, j int) bool {
		if historyKeys[i].userID == historyKeys[j].userID {
			return historyKeys[i].ordinal < historyKeys[j].ordinal
		}
		return historyKeys[i].userID < historyKeys[j].userID
	})
	for _, key := range historyKeys {
		if err := executeCAS(ctx, executor, `DELETE FROM user_password_history
			WHERE user_id = ? AND ordinal = ? AND repository_version = ?`,
			key.userID, key.ordinal, state.persistence.passwordHistory[key]); err != nil {
			return err
		}
	}
	roleKeys := make([]userRoleRowKey, 0, len(state.persistence.userRoles))
	for key := range state.persistence.userRoles {
		user := state.Users[key.userID]
		present := false
		if user != nil {
			for _, role := range user.Roles {
				code, _ := roleCode(role)
				present = present || code == key.roleCode
			}
		}
		if !present {
			roleKeys = append(roleKeys, key)
		}
	}
	sort.Slice(roleKeys, func(i, j int) bool {
		if roleKeys[i].userID == roleKeys[j].userID {
			return roleKeys[i].roleCode < roleKeys[j].roleCode
		}
		return roleKeys[i].userID < roleKeys[j].userID
	})
	for _, key := range roleKeys {
		if err := executeCAS(ctx, executor, `DELETE FROM user_roles
			WHERE user_id = ? AND role_code = ? AND repository_version = ?`,
			key.userID, key.roleCode, state.persistence.userRoles[key]); err != nil {
			return err
		}
	}
	return deleteMissingStringRows(ctx, executor, "users", "id", state.persistence.users, func(key string) bool {
		_, present := state.Users[key]
		return present
	})
}

func deleteMissingStringRows(ctx context.Context, executor workspace.MutationExecutor, table, column string,
	expected map[string]uint64, present func(string) bool) error {
	keys := make([]string, 0, len(expected))
	for key := range expected {
		if !present(key) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		query := `DELETE FROM ` + table + ` WHERE ` + column + ` = ? AND repository_version = ?`
		if err := executeCAS(ctx, executor, query, key, expected[key]); err != nil {
			return err
		}
	}
	return nil
}

func saveUsers(ctx context.Context, executor workspace.MutationExecutor, state repositoryState, now string) error {
	ids := sortedUserIDs(state.Users)
	for _, id := range ids {
		user := state.Users[id]
		status, _ := userStateCode(user.State)
		policy, memory, iterations, parallelism, salt, digest, _ := verifierDatabaseValues(user.Password)
		failureTimes, _ := encodeFailureTimes(user.SignInFailures)
		arguments := []any{
			user.Username, normalizedUsername(user.Username), user.DisplayName, status, user.Version,
			policy, memory, iterations, parallelism, salt, digest,
			nullBytes(user.ActivationHash), nullBytes(user.ActivationConsumedHash), nullBytes(user.ActivationEncrypted),
			nullTime(user.ActivationExpires), user.ActivationFails, nullString(user.ActivationSessionID), failureTimes,
			len(user.SignInFailures), nullTime(user.LockedUntil), now,
		}
		if expected, exists := state.persistence.users[id]; exists {
			arguments = append(arguments, id, expected)
			if err := executeCAS(ctx, executor, `UPDATE users SET email = ?, normalized_username = ?, display_name = ?,
				status = ?, version = ?, password_policy_version = ?, password_memory_kib = ?, password_iterations = ?,
				password_parallelism = ?, password_salt = ?, password_digest = ?, activation_hash = ?,
				activation_consumed_hash = ?, activation_encrypted = ?, activation_expires_at = ?, activation_fails = ?,
				activation_session_id = ?, sign_in_failure_times = ?, failed_attempts = ?, locked_until = ?,
				updated_at = ?, repository_version = repository_version + 1
				WHERE id = ? AND repository_version = ?`, arguments...); err != nil {
				return err
			}
			continue
		}
		arguments = append([]any{id}, arguments...)
		arguments = append(arguments, now)
		if _, err := executor.ExecContext(ctx, `INSERT INTO users(id, email, normalized_username, display_name, status,
			version, password_policy_version, password_memory_kib, password_iterations, password_parallelism,
			password_salt, password_digest, activation_hash, activation_consumed_hash, activation_encrypted,
			activation_expires_at, activation_fails, activation_session_id, sign_in_failure_times, failed_attempts,
			locked_until, updated_at, created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, arguments...); err != nil {
			return classifyRepositoryWrite(err)
		}
	}
	return nil
}

func saveUserRoles(ctx context.Context, executor workspace.MutationExecutor, state repositoryState, now string) error {
	type row struct{ key userRoleRowKey }
	rows := make([]row, 0)
	for userID, user := range state.Users {
		for _, role := range user.Roles {
			code, _ := roleCode(role)
			rows = append(rows, row{key: userRoleRowKey{userID: userID, roleCode: code}})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].key.userID == rows[j].key.userID {
			return rows[i].key.roleCode < rows[j].key.roleCode
		}
		return rows[i].key.userID < rows[j].key.userID
	})
	for _, row := range rows {
		if _, exists := state.persistence.userRoles[row.key]; exists {
			continue
		}
		if _, err := executor.ExecContext(ctx, `INSERT INTO user_roles(user_id, role_code, assigned_at)
			VALUES (?, ?, ?)`, row.key.userID, row.key.roleCode, now); err != nil {
			return classifyRepositoryWrite(err)
		}
	}
	return nil
}

func savePasswordHistory(ctx context.Context, executor workspace.MutationExecutor, state repositoryState) error {
	for _, userID := range sortedUserIDs(state.Users) {
		for ordinal, verifier := range state.Users[userID].PasswordHistory {
			policy, memory, iterations, parallelism, salt, digest, _ := verifierDatabaseValues(verifier)
			key := passwordHistoryRowKey{userID: userID, ordinal: ordinal}
			if expected, exists := state.persistence.passwordHistory[key]; exists {
				if err := executeCAS(ctx, executor, `UPDATE user_password_history SET policy_version = ?, memory_kib = ?,
					iterations = ?, parallelism = ?, salt = ?, digest = ?, repository_version = repository_version + 1
					WHERE user_id = ? AND ordinal = ? AND repository_version = ?`,
					policy, memory, iterations, parallelism, salt, digest, userID, ordinal, expected); err != nil {
					return err
				}
				continue
			}
			if _, err := executor.ExecContext(ctx, `INSERT INTO user_password_history(user_id, ordinal,
				policy_version, memory_kib, iterations, parallelism, salt, digest) VALUES (?,?,?,?,?,?,?,?)`,
				userID, ordinal, policy, memory, iterations, parallelism, salt, digest); err != nil {
				return classifyRepositoryWrite(err)
			}
		}
	}
	return nil
}

func saveApplicationSessions(ctx context.Context, executor workspace.MutationExecutor, state repositoryState) error {
	ids := sortedSessionIDs(state.Sessions)
	sort.SliceStable(ids, func(i, j int) bool {
		return state.Sessions[ids[i]].State != tammyv1.SessionState_SESSION_STATE_ACTIVE &&
			state.Sessions[ids[j]].State == tammyv1.SessionState_SESSION_STATE_ACTIVE
	})
	for _, id := range ids {
		session := state.Sessions[id]
		arguments := []any{session.UserID, int32(session.State), requiredTime(session.CreatedAt),
			requiredTime(session.LastActive), requiredTime(session.ExpiresAt), nullTime(session.EndedAt)}
		if expected, exists := state.persistence.sessions[id]; exists {
			arguments = append(arguments, id, expected)
			if err := executeCAS(ctx, executor, `UPDATE application_sessions SET user_id = ?, state = ?, created_at = ?,
				last_active_at = ?, expires_at = ?, ended_at = ?, repository_version = repository_version + 1
				WHERE id = ? AND repository_version = ?`, arguments...); err != nil {
				return err
			}
			continue
		}
		arguments = append([]any{id}, arguments...)
		if _, err := executor.ExecContext(ctx, `INSERT INTO application_sessions(id, user_id, state, created_at,
			last_active_at, expires_at, ended_at) VALUES (?,?,?,?,?,?,?)`, arguments...); err != nil {
			return classifyRepositoryWrite(err)
		}
	}
	return nil
}

func saveTOTPFactors(ctx context.Context, executor workspace.MutationExecutor, state repositoryState) error {
	ids := sortedFactorIDs(state.Factors)
	for _, id := range ids {
		factor := state.Factors[id]
		arguments := []any{factor.UserID, factor.Version, int32(factor.State), requiredTime(factor.CreatedAt),
			nullBytes(factor.EncryptedSecret), factor.LastCounter}
		if expected, exists := state.persistence.factors[id]; exists {
			arguments = append(arguments, id, expected)
			if err := executeCAS(ctx, executor, `UPDATE totp_factors SET user_id = ?, version = ?, state = ?,
				created_at = ?, encrypted_secret = ?, last_counter = ?, repository_version = repository_version + 1
				WHERE id = ? AND repository_version = ?`, arguments...); err != nil {
				return err
			}
			continue
		}
		arguments = append([]any{id}, arguments...)
		if _, err := executor.ExecContext(ctx, `INSERT INTO totp_factors(id, user_id, version, state, created_at,
			encrypted_secret, last_counter) VALUES (?,?,?,?,?,?,?)`, arguments...); err != nil {
			return classifyRepositoryWrite(err)
		}
	}
	return nil
}

func saveFactorAssertions(ctx context.Context, executor workspace.MutationExecutor, state repositoryState) error {
	ids := sortedAssertionIDs(state.Assertions)
	sort.SliceStable(ids, func(i, j int) bool {
		return state.Assertions[ids[i]].Consumed && !state.Assertions[ids[j]].Consumed
	})
	for _, id := range ids {
		assertion := state.Assertions[id]
		consumed := 0
		if assertion.Consumed {
			consumed = 1
		}
		arguments := []any{assertion.UserID, assertion.SessionID, assertion.Purpose,
			requiredTime(assertion.Asserted), consumed}
		if expected, exists := state.persistence.assertions[id]; exists {
			arguments = append(arguments, id, expected)
			if err := executeCAS(ctx, executor, `UPDATE factor_assertions SET user_id = ?, session_id = ?,
				purpose = ?, asserted_at = ?, consumed = ?, repository_version = repository_version + 1
				WHERE id = ? AND repository_version = ?`, arguments...); err != nil {
				return err
			}
			continue
		}
		arguments = append([]any{id}, arguments...)
		if _, err := executor.ExecContext(ctx, `INSERT INTO factor_assertions(id, user_id, session_id, purpose,
			asserted_at, consumed) VALUES (?,?,?,?,?,?)`, arguments...); err != nil {
			return classifyRepositoryWrite(err)
		}
	}
	return nil
}

func saveCommandIdempotency(ctx context.Context, executor workspace.MutationExecutor, state repositoryState, now string) error {
	keys := make([]string, 0, len(state.Idempotency))
	for key := range state.Idempotency {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, retained := state.persistence.idempotency[key]; retained {
			continue
		}
		record := state.Idempotency[key]
		if _, err := executor.ExecContext(ctx, `INSERT INTO command_idempotency(operation_key, command_type,
			semantic_sha256, actor_user_id, user_id, factor_id, session_id, response_encrypted, created_at)
			VALUES (?,?,?,?,?,?,?,?,?)`, key, record.Command, record.SemanticHash, nullString(record.ActorUserID),
			nullString(record.UserID), nullString(record.FactorID), nullString(record.SessionID),
			nullBytes(record.ResponseEncrypted), now); err != nil {
			return classifyRepositoryWrite(err)
		}
	}
	return nil
}

func verifierDatabaseValues(verifier workspace.PasswordVerifier) (any, any, any, any, any, any, error) {
	missing := verifier.PolicyVersion == 0 && verifier.MemoryKiB == 0 && verifier.Iterations == 0 &&
		verifier.Parallelism == 0 && verifier.Salt == nil && verifier.Digest == nil
	if missing {
		return nil, nil, nil, nil, nil, nil, nil
	}
	if verifier.PolicyVersion != 1 || verifier.MemoryKiB != 64*1024 || verifier.Iterations != 3 ||
		verifier.Parallelism != 1 || len(verifier.Salt) != 16 || len(verifier.Digest) != 32 {
		return nil, nil, nil, nil, nil, nil, ErrRepositoryIntegrity
	}
	return verifier.PolicyVersion, verifier.MemoryKiB, verifier.Iterations, verifier.Parallelism,
		append([]byte(nil), verifier.Salt...), append([]byte(nil), verifier.Digest...), nil
}

func encodeFailureTimes(values []time.Time) ([]byte, error) {
	if len(values) > 5 {
		return nil, ErrRepositoryIntegrity
	}
	encoded := make([]byte, len(values)*8)
	for index, value := range values {
		if value.IsZero() {
			return nil, ErrRepositoryIntegrity
		}
		binary.BigEndian.PutUint64(encoded[index*8:], uint64(value.UTC().UnixNano()))
	}
	return encoded, nil
}

func requiredTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

func nullTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return requiredTime(value)
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullBytes(value []byte) any {
	if value == nil {
		return nil
	}
	return value
}

func executeCAS(ctx context.Context, executor workspace.MutationExecutor, query string, arguments ...any) error {
	result, err := executor.ExecContext(ctx, query, arguments...)
	if err != nil {
		return classifyRepositoryWrite(err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return ErrRepositoryIntegrity
	}
	if affected != 1 {
		return ErrRepositoryConflict
	}
	return nil
}

func classifyRepositoryWrite(err error) error {
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	if isRepositoryBusy(err) || strings.Contains(message, "unique constraint failed") ||
		strings.Contains(message, "primary key") {
		return ErrRepositoryConflict
	}
	return ErrRepositoryIntegrity
}

func isRepositoryBusy(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "database is locked") || strings.Contains(message, "database is busy")
}

func sortedUserIDs(records map[string]*userRecord) []string {
	ids := make([]string, 0, len(records))
	for id := range records {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func sortedSessionIDs(records map[string]*sessionRecord) []string {
	ids := make([]string, 0, len(records))
	for id := range records {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func sortedFactorIDs(records map[string]*factorRecord) []string {
	ids := make([]string, 0, len(records))
	for id := range records {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func sortedAssertionIDs(records map[string]*assertionRecord) []string {
	ids := make([]string, 0, len(records))
	for id := range records {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
