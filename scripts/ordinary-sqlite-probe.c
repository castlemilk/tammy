#include "sqlite3.h"

int main(int argc, char **argv) {
  sqlite3 *database = 0;
  sqlite3_stmt *statement = 0;
  int result;

  if (argc != 2) return 64;
  result = sqlite3_open_v2(argv[1], &database, SQLITE_OPEN_READONLY | SQLITE_OPEN_NOMUTEX, 0);
  if (result != SQLITE_OK) {
    if (database != 0) sqlite3_close(database);
    return 2;
  }
  result = sqlite3_prepare_v2(database, "PRAGMA schema_version", -1, &statement, 0);
  if (result == SQLITE_OK) result = sqlite3_step(statement);
  if (statement != 0) sqlite3_finalize(statement);
  sqlite3_close(database);
  return result == SQLITE_ROW ? 0 : 3;
}
