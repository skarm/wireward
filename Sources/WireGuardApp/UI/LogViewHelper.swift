// SPDX-License-Identifier: MIT
// Copyright © 2018-2023 WireGuard LLC. All Rights Reserved.

import Foundation

@MainActor
public final class LogViewHelper {
    private let logFilePath: String
    private var fetching = false
    var cursor: UInt32 = UINT32_MAX
    nonisolated static let formatOptions: ISO8601DateFormatter.Options = [
        .withYear, .withMonth, .withDay, .withTime,
        .withDashSeparatorInDate, .withColonSeparatorInTime, .withSpaceBetweenDateAndTime,
        .withFractionalSeconds
    ]

    struct LogEntry: Sendable {
        let timestamp: String
        let message: String

        func text() -> String {
            return timestamp + " " + message
        }
    }

    class LogEntries {
        var entries: [LogEntry] = []
    }

    init?(logFilePath: String?) {
        guard let logFilePath = logFilePath else { return nil }
        guard let log = open_log(logFilePath) else { return nil }
        close_log(log)
        self.logFilePath = logFilePath
    }

    func fetchLogEntriesSinceLastFetch(completion: @escaping @MainActor ([LogViewHelper.LogEntry]) -> Void) {
        guard !fetching else { return }
        fetching = true
        let cursor = cursor
        let logFilePath = logFilePath
        DispatchQueue.global(qos: .userInitiated).async { [weak self] in
            guard let log = open_log(logFilePath) else {
                DispatchQueue.main.async { self?.fetching = false; completion([]) }
                return
            }
            defer { close_log(log) }
            let logEntries = LogEntries()
            // The C function invokes its callback synchronously; keep the object alive
            // for that call and pass its object pointer, not the address of a Swift reference.
            let context = Unmanaged.passUnretained(logEntries).toOpaque()
            let newCursor = view_lines_from_cursor(log, cursor, context) { cStr, timestamp, ctx in
                guard let ctx = ctx else { return }
                let entries = Unmanaged<LogEntries>.fromOpaque(ctx).takeUnretainedValue()
                let message = cStr.map { String(cString: $0) } ?? ""
                let date = Date(timeIntervalSince1970: Double(timestamp) / 1000000000)
                let dateString = ISO8601DateFormatter.string(from: date, timeZone: TimeZone.current, formatOptions: LogViewHelper.formatOptions)
                entries.entries.append(LogEntry(timestamp: dateString, message: message))
            }
            let entries = logEntries.entries
            DispatchQueue.main.async { [weak self] in
                guard let self = self else { return }
                self.cursor = newCursor
                self.fetching = false
                completion(entries)
            }
        }
    }
}
