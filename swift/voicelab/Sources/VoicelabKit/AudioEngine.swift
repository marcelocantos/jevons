// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import AVFAudio
import AVFoundation
import Foundation

/// AVAudioEngine wrapper that delivers AEC + NS + AGC for free via
/// `setVoiceProcessingEnabled(true)` on the input node — the same
/// audio unit FaceTime uses. Capture frames are converted to 24 kHz
/// PCM16 mono (Grok's wire format) and surfaced through `onCapture`;
/// playback consumes int16 frames at the wire format and converts up
/// to the hardware output format for the player node.
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
    public var onCapture: ((Data) -> Void)?

    private let engine = AVAudioEngine()
    private let playerNode = AVAudioPlayerNode()

    // Mic-side: post-VP hardware format → 24 kHz PCM16 mono wire
    // format. Post-VP the input is multi-channel float32 at hardware
    // rate; AVAudioConverter handles both the channel collapse and
    // the rate conversion.
    private let captureFormat: AVAudioFormat
    private let wireFormat: AVAudioFormat
    private let captureConverter: AVAudioConverter

    // Player connects at the hardware output format (typically 2 ch
    // 48 kHz Float32 deinterleaved). Anything else wedges
    // engine.start with -10875 under voice processing. We convert
    // Grok wire-format audio up to the player format in-process
    // before scheduling.
    private let playerFormat: AVAudioFormat
    private let playerConverter: AVAudioConverter

    public init() throws {
        guard let wire = AVAudioFormat(commonFormat: .pcmFormatInt16,
                                       sampleRate: AudioEngine.sampleRate,
                                       channels: 1,
                                       interleaved: true) else {
            throw AudioError.formatUnsupported("24kHz mono int16")
        }
        self.wireFormat = wire

        // Voice processing must be enabled BEFORE the input node's
        // format is queried — the AU swaps its native format on that
        // call and querying first locks in the wrong shape.
        do {
            try engine.inputNode.setVoiceProcessingEnabled(true)
        } catch {
            throw AudioError.voiceProcessing(error)
        }
        self.captureFormat = engine.inputNode.outputFormat(forBus: 0)

        guard let cc = AVAudioConverter(from: captureFormat, to: wire) else {
            throw AudioError.formatUnsupported(
                "no converter from \(captureFormat) to \(wire)"
            )
        }
        self.captureConverter = cc

        self.playerFormat = engine.outputNode.outputFormat(forBus: 0)
        guard let pc = AVAudioConverter(from: wire, to: playerFormat) else {
            throw AudioError.formatUnsupported(
                "no converter from \(wire) to \(playerFormat)"
            )
        }
        self.playerConverter = pc

        engine.attach(playerNode)
        engine.connect(playerNode, to: engine.outputNode, format: playerFormat)

        engine.inputNode.installTap(onBus: 0, bufferSize: 1024,
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

    // MARK: - Capture pipeline

    private func handleCaptureBuffer(_ inputBuffer: AVAudioPCMBuffer) {
        guard let handler = onCapture else { return }

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
