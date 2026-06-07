// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import AVFAudio
import AVFoundation
import Foundation

/// AVAudioEngine wrapper that delivers AEC + NS + AGC for free via
/// the `setVoiceProcessingEnabled(true)` flag on the input node — the
/// same audio unit FaceTime uses. Capture frames are converted to
/// 24 kHz PCM16 mono (Grok's wire format) and surfaced through a
/// callback; playback consumes int16 frames into the player node.
public final class AudioEngine {
    public enum AudioError: LocalizedError {
        case formatUnsupported(String)
        case voiceProcessing(Error)
        case engineStart(Error)

        public var errorDescription: String? {
            switch self {
            case .formatUnsupported(let msg): "audio: format unsupported (\(msg))"
            case .voiceProcessing(let err): "audio: voice processing setup failed (\(err.localizedDescription))"
            case .engineStart(let err): "audio: engine start failed (\(err.localizedDescription))"
            }
        }
    }

    public static let sampleRate: Double = 24000

    /// Fires on a non-main queue with PCM16 mono 24 kHz frames.
    /// Gated by `captureEnabled` so a push-to-talk host can mute the
    /// mic between turns without tearing down the engine.
    public var onCapture: ((Data) -> Void)?

    /// When false, captured frames are dropped at the source before
    /// being forwarded. Toggle freely from any thread.
    private let captureEnabledFlag = AtomicBool(false)
    public var captureEnabled: Bool {
        get { captureEnabledFlag.value }
        set { captureEnabledFlag.value = newValue }
    }

    private let engine = AVAudioEngine()
    private let playerNode = AVAudioPlayerNode()

    // Mic-side conversion: hardware (post-VP) format → 24 kHz PCM16
    // mono wire format. Post-VP the input is multi-channel float32
    // at hardware rate; AVAudioConverter handles both the channel
    // collapse and the rate conversion.
    private let captureFormat: AVAudioFormat
    private let wireFormat: AVAudioFormat
    private let captureConverter: AVAudioConverter

    // Player connects at the hardware output format (typically 2 ch
    // 48 kHz Float32 deinterleaved). Going through any
    // non-hardware-matching format wedges engine.start with -10875
    // when voice processing is enabled. We accept Grok audio at 24
    // kHz mono int16 and convert in-process to the player format
    // before scheduling.
    private let playerFormat: AVAudioFormat
    private let playerConverter: AVAudioConverter

    public init() throws {
        // Build the 24kHz PCM16 mono wire format up front; we use it
        // both as the converter output and the public capture format.
        guard let wire = AVAudioFormat(commonFormat: .pcmFormatInt16,
                                       sampleRate: AudioEngine.sampleRate,
                                       channels: 1,
                                       interleaved: true) else {
            throw AudioError.formatUnsupported("24kHz mono int16")
        }
        self.wireFormat = wire

        // Voice processing must be enabled BEFORE the input node's
        // format is queried — the AU swaps its native format when
        // voice processing turns on (typically to a normalised 16-bit
        // mono stream), and querying the format before flipping the
        // flag locks in the hardware format we don't want.
        do {
            try engine.inputNode.setVoiceProcessingEnabled(true)
        } catch {
            throw AudioError.voiceProcessing(error)
        }

        self.captureFormat = engine.inputNode.outputFormat(forBus: 0)

        guard let converter = AVAudioConverter(from: captureFormat, to: wire) else {
            throw AudioError.formatUnsupported(
                "no converter from \(captureFormat) to \(wire)"
            )
        }
        self.captureConverter = converter

        // Match the hardware output format exactly. Anything else
        // wedges engine.start with -10875 under voice processing.
        self.playerFormat = engine.outputNode.outputFormat(forBus: 0)

        // Grok wire format → player format. AVAudioConverter handles
        // both the channel widen (mono → stereo) and the rate
        // conversion (24 kHz → hardware rate).
        guard let pc = AVAudioConverter(from: wire, to: playerFormat) else {
            throw AudioError.formatUnsupported(
                "no converter from \(wire) to \(playerFormat)"
            )
        }
        self.playerConverter = pc

        engine.attach(playerNode)
        engine.connect(playerNode, to: engine.outputNode, format: playerFormat)

        // Install the tap LAST so it sees the post-VP format.
        let bufferSize: AVAudioFrameCount = 1024
        engine.inputNode.installTap(onBus: 0, bufferSize: bufferSize,
                                     format: captureFormat) { [weak self] buffer, _ in
            self?.handleCaptureBuffer(buffer)
        }
    }

    public func start() throws {
        engine.prepare()
        do {
            try engine.start()
        } catch {
            throw AudioError.engineStart(error)
        }
        playerNode.play()
    }

    public func stop() {
        playerNode.stop()
        engine.stop()
        engine.inputNode.removeTap(onBus: 0)
    }

    /// Enqueue PCM16 mono 24 kHz bytes for playback.
    public func play(_ pcm: Data) {
        guard !pcm.isEmpty else { return }
        guard let buffer = makePlaybackBuffer(from: pcm) else { return }
        playerNode.scheduleBuffer(buffer)
    }

    /// Drop everything queued for playback — used for barge-in so the
    /// last sentence Grok was mid-saying gets cut off instantly when
    /// the user takes the floor.
    public func clearPlayback() {
        playerNode.stop()
        playerNode.play()
    }

    // MARK: - Capture pipeline

    private func handleCaptureBuffer(_ inputBuffer: AVAudioPCMBuffer) {
        guard captureEnabledFlag.value else { return }
        guard let handler = onCapture else { return }

        // Worst case 1:1 frames; we conservatively size up because
        // upsampling shouldn't happen here (hardware ≥ 24 kHz almost
        // always), but downsampling certainly does.
        let outFrames = AVAudioFrameCount(
            Double(inputBuffer.frameLength) * AudioEngine.sampleRate / captureFormat.sampleRate
        ) + 16
        guard let outBuffer = AVAudioPCMBuffer(pcmFormat: wireFormat,
                                                frameCapacity: outFrames) else {
            return
        }

        var error: NSError?
        var fed = false
        let status = captureConverter.convert(
            to: outBuffer,
            error: &error
        ) { _, statusPtr in
            if fed {
                statusPtr.pointee = .noDataNow
                return nil
            }
            fed = true
            statusPtr.pointee = .haveData
            return inputBuffer
        }
        guard status != .error, error == nil,
              let int16 = outBuffer.int16ChannelData else {
            return
        }

        let frameLen = Int(outBuffer.frameLength)
        guard frameLen > 0 else { return }
        let byteLen = frameLen * MemoryLayout<Int16>.size
        let data = Data(bytes: int16[0], count: byteLen)
        handler(data)
    }

    private func makePlaybackBuffer(from pcm: Data) -> AVAudioPCMBuffer? {
        let inSamples = pcm.count / MemoryLayout<Int16>.size
        guard inSamples > 0 else { return nil }

        guard let inBuffer = AVAudioPCMBuffer(pcmFormat: wireFormat,
                                               frameCapacity: AVAudioFrameCount(inSamples)) else {
            return nil
        }
        inBuffer.frameLength = AVAudioFrameCount(inSamples)
        guard let inInt16 = inBuffer.int16ChannelData?[0] else { return nil }
        pcm.withUnsafeBytes { raw in
            guard let src = raw.bindMemory(to: Int16.self).baseAddress else { return }
            inInt16.update(from: src, count: inSamples)
        }

        // Output frame count after sample-rate conversion. Add a few
        // frames of slack — the converter sometimes wants one extra
        // for boundary samples and undersized outputs cause errors.
        let ratio = playerFormat.sampleRate / wireFormat.sampleRate
        let outFrames = AVAudioFrameCount(Double(inSamples) * ratio) + 16
        guard let outBuffer = AVAudioPCMBuffer(pcmFormat: playerFormat,
                                                frameCapacity: outFrames) else {
            return nil
        }

        var error: NSError?
        var fed = false
        _ = playerConverter.convert(to: outBuffer, error: &error) { _, status in
            if fed {
                status.pointee = .noDataNow
                return nil
            }
            fed = true
            status.pointee = .haveData
            return inBuffer
        }
        if error != nil { return nil }
        return outBuffer
    }
}

/// Tiny atomic boolean — saves pulling in Swift Atomics for a single
/// flag. CAS isn't needed; we only swap, not compare-and-swap.
final class AtomicBool: @unchecked Sendable {
    private let lock = NSLock()
    private var _value: Bool
    init(_ initial: Bool) { _value = initial }
    var value: Bool {
        get { lock.withLock { _value } }
        set { lock.withLock { _value = newValue } }
    }
}
