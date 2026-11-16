import Foundation
#if canImport(Darwin)
import Darwin
#elseif canImport(Glibc)
import Glibc
#endif

/// Private Application Support/VoiceRewrite/settings.json persistence.
/// Uses no-follow descriptor-relative POSIX I/O and atomic rename in one directory.
public final class SettingsStore {
    public let directory: URL
    private let beforeReplace: () throws -> Void
    private static let filename = "settings.json"
    private static let maxBytes = 64 * 1024

    public convenience init() throws {
        guard let support = FileManager.default.urls(for: .applicationSupportDirectory,
                                                      in: .userDomainMask).first else {
            throw SettingsError.insecureStorage
        }
        self.init(directory: support.appendingPathComponent("VoiceRewrite", isDirectory: true))
    }

    public init(directory: URL) {
        self.directory = directory
        self.beforeReplace = {}
    }

    // Failure injection is internal and never used by the production initializer.
    internal init(directory: URL, beforeReplace: @escaping () throws -> Void) {
        self.directory = directory
        self.beforeReplace = beforeReplace
    }

    public func load() throws -> MacSettings {
        let dir = try openPrivateDirectory()
        defer { _ = close(dir) }
        let file = openat(dir, Self.filename, O_RDONLY | O_CLOEXEC | O_NOFOLLOW)
        if file < 0 {
            if errno == ENOENT { return .defaults }
            throw SettingsError.io(operation: "open settings", code: errno)
        }
        defer { _ = close(file) }
        try checkPrivateFile(file)
        var contents = Data()
        var chunk = [UInt8](repeating: 0, count: 4096)
        while true {
            let count = chunk.withUnsafeMutableBytes { read(file, $0.baseAddress, $0.count) }
            if count < 0 {
                if errno == EINTR { continue }
                throw SettingsError.io(operation: "read settings", code: errno)
            }
            if count == 0 { break }
            guard contents.count + count <= Self.maxBytes else { throw SettingsError.fileTooLarge }
            contents.append(contentsOf: chunk[..<count])
        }
        return try MacSettings.decodeStrict(contents)
    }

    public func save(_ settings: MacSettings) throws {
        var settings = settings
        try settings.canonicalize()
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
        var bytes = try encoder.encode(settings)
        bytes.append(0x0A)
        guard bytes.count <= Self.maxBytes else { throw SettingsError.fileTooLarge }

        let dir = try openPrivateDirectory()
        defer { _ = close(dir) }
        // An existing symlink, non-regular file or shared-permission file is not safe to replace.
        var existing = stat()
        if fstatat(dir, Self.filename, &existing, AT_SYMLINK_NOFOLLOW) == 0 {
            guard (existing.st_mode & mode_t(S_IFMT)) == mode_t(S_IFREG),
                  (existing.st_mode & 0o077) == 0,
                  existing.st_uid == geteuid() else { throw SettingsError.insecureStorage }
        } else if errno != ENOENT {
            throw SettingsError.io(operation: "inspect settings", code: errno)
        }

        let tempName = ".settings-\(UUID().uuidString).tmp"
        let temp = openat(dir, tempName, O_WRONLY | O_CREAT | O_EXCL | O_NOFOLLOW | O_CLOEXEC, 0o600)
        guard temp >= 0 else { throw SettingsError.io(operation: "create temporary settings", code: errno) }
        defer {
            _ = close(temp)
            _ = unlinkat(dir, tempName, 0) // harmless after successful rename
        }
        try bytes.withUnsafeBytes { buffer in
            guard let base = buffer.baseAddress else { return }
            var offset = 0
            while offset < buffer.count {
                let written = write(temp, base.advanced(by: offset), buffer.count - offset)
                if written < 0 {
                    if errno == EINTR { continue }
                    throw SettingsError.io(operation: "write temporary settings", code: errno)
                }
                if written == 0 { throw SettingsError.io(operation: "write temporary settings", code: EIO) }
                offset += written
            }
        }
        guard fsync(temp) == 0 else { throw SettingsError.io(operation: "sync temporary settings", code: errno) }
        try beforeReplace()
        guard renameat(dir, tempName, dir, Self.filename) == 0 else {
            throw SettingsError.io(operation: "replace settings", code: errno)
        }
        // A sync failure after rename cannot undo an already-valid atomic replacement.
        guard fsync(dir) == 0 else { throw SettingsError.io(operation: "sync settings directory", code: errno) }
    }

    private func openPrivateDirectory() throws -> Int32 {
        guard directory.isFileURL else { throw SettingsError.insecureStorage }
        do {
            try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true,
                                                    attributes: [.posixPermissions: 0o700])
        } catch {
            throw SettingsError.io(operation: "create settings directory", code: (error as NSError).code == NSFileWriteNoPermissionError ? EACCES : EIO)
        }
        let fd = open(directory.path, O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC)
        guard fd >= 0 else { throw SettingsError.io(operation: "open settings directory", code: errno) }
        var info = stat()
        guard fstat(fd, &info) == 0,
              (info.st_mode & mode_t(S_IFMT)) == mode_t(S_IFDIR),
              (info.st_mode & 0o077) == 0,
              info.st_uid == geteuid() else {
            _ = close(fd)
            throw SettingsError.insecureStorage
        }
        return fd
    }

    private func checkPrivateFile(_ fd: Int32) throws {
        var info = stat()
        guard fstat(fd, &info) == 0,
              (info.st_mode & mode_t(S_IFMT)) == mode_t(S_IFREG),
              (info.st_mode & 0o077) == 0,
              info.st_uid == geteuid() else { throw SettingsError.insecureStorage }
        guard info.st_size <= Self.maxBytes else { throw SettingsError.fileTooLarge }
    }
}
