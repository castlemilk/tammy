import AppKit
import Foundation

private enum BookmarkTestError: Error {
  case cancelled
  case invalidArguments
  case invalidBookmark
  case pathMismatch
  case readFailed
  case helperFailed
}

private struct BookmarkPayload: Codable {
  let bookmark: String
  let selectedPath: String
}

private func runHelper() throws {
  let input = FileHandle.standardInput.readDataToEndOfFile()
  guard input.count <= 96 * 1024 else { throw BookmarkTestError.invalidArguments }
  let payload = try JSONDecoder().decode(BookmarkPayload.self, from: input)
  guard let bookmark = Data(base64Encoded: payload.bookmark), bookmark.count <= 64 * 1024
  else { throw BookmarkTestError.invalidArguments }

  var stale = false
  let selectedPath = payload.selectedPath
  let selectedURL = try URL(
    resolvingBookmarkData: bookmark,
    options: [.withSecurityScope, .withoutUI],
    relativeTo: nil,
    bookmarkDataIsStale: &stale
  )
  guard !stale else { throw BookmarkTestError.invalidBookmark }
  guard
    selectedURL.standardizedFileURL.path
      == URL(fileURLWithPath: selectedPath).standardizedFileURL.path
  else { throw BookmarkTestError.pathMismatch }
  guard selectedURL.startAccessingSecurityScopedResource() else {
    throw BookmarkTestError.invalidBookmark
  }
  defer { selectedURL.stopAccessingSecurityScopedResource() }

  let handle = try FileHandle(forReadingFrom: selectedURL)
  defer { try? handle.close() }
  guard try handle.read(upToCount: 1)?.isEmpty == false else { throw BookmarkTestError.readFailed }
  FileHandle.standardOutput.write(Data("SBR_SECURITY_BOOKMARK_OK\n".utf8))
}

@MainActor
private func runHost() throws {
  let arguments = CommandLine.arguments
  guard
    let helperIndex = arguments.firstIndex(of: "--helper-path"),
    let expectedPathIndex = arguments.firstIndex(of: "--expected-path"),
    helperIndex + 1 < arguments.count,
    expectedPathIndex + 1 < arguments.count
  else { throw BookmarkTestError.invalidArguments }

  let application = NSApplication.shared
  application.setActivationPolicy(.regular)
  application.activate(ignoringOtherApps: true)
  let panel = NSOpenPanel()
  panel.allowsMultipleSelection = false
  panel.canChooseDirectories = false
  panel.canChooseFiles = true
  panel.resolvesAliases = false
  guard panel.runModal() == .OK, let selectedURL = panel.url else {
    throw BookmarkTestError.cancelled
  }
  guard
    selectedURL.standardizedFileURL.path
      == URL(fileURLWithPath: arguments[expectedPathIndex + 1]).standardizedFileURL.path
  else { throw BookmarkTestError.pathMismatch }
  let bookmark = try selectedURL.bookmarkData(
    options: .withSecurityScope,
    includingResourceValuesForKeys: nil,
    relativeTo: nil)
  guard bookmark.count <= 64 * 1024 else { throw BookmarkTestError.invalidBookmark }

  let helper = Process()
  let input = Pipe()
  let output = Pipe()
  helper.executableURL = URL(fileURLWithPath: arguments[helperIndex + 1])
  helper.arguments = ["--helper"]
  helper.standardInput = input
  helper.standardOutput = output
  try helper.run()
  let payload = BookmarkPayload(
    bookmark: bookmark.base64EncodedString(),
    selectedPath: selectedURL.path
  )
  input.fileHandleForWriting.write(try JSONEncoder().encode(payload))
  try input.fileHandleForWriting.close()
  helper.waitUntilExit()
  let result = output.fileHandleForReading.readDataToEndOfFile()
  guard helper.terminationStatus == 0, result == Data("SBR_SECURITY_BOOKMARK_OK\n".utf8) else {
    throw BookmarkTestError.helperFailed
  }
  FileHandle.standardOutput.write(result)
}

do {
  if CommandLine.arguments.contains("--helper") {
    try runHelper()
  } else {
    try MainActor.assumeIsolated { try runHost() }
  }
} catch {
  FileHandle.standardError.write(Data("SBR_SECURITY_BOOKMARK_FAILED\n".utf8))
  exit(1)
}
