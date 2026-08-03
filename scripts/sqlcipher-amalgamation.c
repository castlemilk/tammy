#include "sqlite3.c"

#include <stdint.h>

int tammy_sqlite3_main_file_matches(sqlite3 *database, sqlite3_uint64 retainedHandle) {
  sqlite3_file *sqliteFile = 0;
  int result;

  if (database == 0) return 0;
  result = sqlite3_file_control(
    database,
    "main",
    SQLITE_FCNTL_FILE_POINTER,
    (void *)&sqliteFile
  );
  if (result != SQLITE_OK || sqliteFile == 0) return 0;

#if SQLITE_OS_UNIX
  {
    unixFile *nativeFile = (unixFile *)sqliteFile;
    struct stat nativeIdentity;
    struct stat retainedIdentity;

    if (retainedHandle > INT_MAX || nativeFile->h < 0) return 0;
    if (fstat(nativeFile->h, &nativeIdentity) != 0) return 0;
    if (fstat((int)retainedHandle, &retainedIdentity) != 0) return 0;
    return nativeIdentity.st_dev == retainedIdentity.st_dev &&
      nativeIdentity.st_ino == retainedIdentity.st_ino;
  }
#elif SQLITE_OS_WIN
  {
    winFile *nativeFile = (winFile *)sqliteFile;
    HANDLE retainedFile = (HANDLE)(uintptr_t)retainedHandle;
    BY_HANDLE_FILE_INFORMATION nativeIdentity;
    BY_HANDLE_FILE_INFORMATION retainedIdentity;

    if (nativeFile->h == 0 || nativeFile->h == INVALID_HANDLE_VALUE) return 0;
    if (retainedFile == 0 || retainedFile == INVALID_HANDLE_VALUE) return 0;
    if (!GetFileInformationByHandle(nativeFile->h, &nativeIdentity)) return 0;
    if (!GetFileInformationByHandle(retainedFile, &retainedIdentity)) return 0;
    return nativeIdentity.dwVolumeSerialNumber == retainedIdentity.dwVolumeSerialNumber &&
      nativeIdentity.nFileIndexHigh == retainedIdentity.nFileIndexHigh &&
      nativeIdentity.nFileIndexLow == retainedIdentity.nFileIndexLow;
  }
#else
  (void)retainedHandle;
  return 0;
#endif
}
