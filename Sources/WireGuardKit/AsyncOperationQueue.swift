// SPDX-License-Identifier: MIT

import Foundation

/// A single consumer preserves submission order across suspension points.
/// Finishing drains queued work, including the adapter's final cleanup.
final class AsyncOperationQueue: Sendable {
    private let continuation: AsyncStream<@Sendable () async -> Void>.Continuation
    private let worker: Task<Void, Never>

    init() {
        let (stream, continuation) = AsyncStream<@Sendable () async -> Void>.makeStream()
        self.continuation = continuation
        worker = Task.detached {
            for await operation in stream { await operation() }
        }
    }

    func submit(_ operation: @escaping @Sendable () async -> Void) { continuation.yield(operation) }
    func finish() { continuation.finish() }
    deinit { continuation.finish() }
}

/// NetworkExtension can omit or deliver its callback after the deadline.
/// Resolve the continuation once, outside the lock, on every completion path.
final class CallbackResult: Sendable {
    private enum State: Sendable {
        case waiting(CheckedContinuation<Void, Error>?)
        case finished(Result<Void, Error>)
    }
    private let state = LockedValue<State>(.waiting(nil))

    func install(_ continuation: CheckedContinuation<Void, Error>) -> Bool {
        let result = state.withLock { state -> Result<Void, Error>? in
            switch state {
            case .waiting: state = .waiting(continuation); return nil
            case .finished(let result): return result
            }
        }
        if let result { continuation.resume(with: result); return false }
        return true
    }

    func resolve(_ result: Result<Void, Error>) {
        let continuation = state.withLock { state -> CheckedContinuation<Void, Error>? in
            guard case .waiting(let continuation) = state else { return nil }
            state = .finished(result)
            return continuation
        }
        continuation?.resume(with: result)
    }

    static func wait(timeoutNanoseconds: UInt64,
                     operation: @Sendable (@escaping @Sendable (Error?) -> Void) -> Void) async throws {
        let result = CallbackResult()
        let timeout = Task {
            do {
                try await Task.sleep(nanoseconds: timeoutNanoseconds)
                result.resolve(.failure(WireGuardAdapterError.networkSettingsTimedOut))
            } catch { /* A successful callback cancels the deadline task. */ }
        }
        defer { timeout.cancel() }
        try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { continuation in
                guard result.install(continuation) else { return }
                operation { error in
                    result.resolve(error.map { .failure($0) } ?? .success(()))
                }
            }
        } onCancel: {
            result.resolve(.failure(CancellationError()))
        }
    }
}
