// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import Foundation

/// LocalVAD detects "the user is speaking" from PCM16 mono frames
/// using a single dBFS threshold with sustain + cooldown hysteresis.
/// The mic frames we feed it are the AEC-cleaned signal from the
/// VoiceProcessingIO audio unit, so playback echo and steady fan noise
/// are already attenuated — a plain RMS threshold is enough.
public struct LocalVAD {
    /// Activation threshold in dBFS. Energy above this for
    /// `triggerSustainMs` continuous milliseconds fires the trigger.
    /// -38 dBFS is a usable middle for built-in iPad mics in moderate
    /// ambience; tune in the car if false positives dominate.
    public var triggerDB: Double
    public var triggerSustainMs: Int
    /// After firing, ignore further detections for this long so we
    /// don't keep re-triggering while the same utterance is mid-stream.
    public var cooldownMs: Int

    public init(triggerDB: Double = -38, triggerSustainMs: Int = 150, cooldownMs: Int = 1500) {
        self.triggerDB = triggerDB
        self.triggerSustainMs = triggerSustainMs
        self.cooldownMs = cooldownMs
    }

    private var sustainedMs: Double = 0
    private var cooldownRemainingMs: Double = 0

    public mutating func ingest(_ pcm: Data, sampleRate: Double) -> Bool {
        let samples = pcm.count / MemoryLayout<Int16>.size
        guard samples > 0 else { return false }
        let durationMs = Double(samples) / sampleRate * 1000

        if cooldownRemainingMs > 0 {
            cooldownRemainingMs = max(0, cooldownRemainingMs - durationMs)
            return false
        }

        var sumSquares: Double = 0
        pcm.withUnsafeBytes { raw in
            let p = raw.bindMemory(to: Int16.self)
            for v in p {
                let f = Double(v)
                sumSquares += f * f
            }
        }
        let rms = (sumSquares / Double(samples)).squareRoot()
        let dB = rms > 0 ? 20 * log10(rms / 32767.0) : -Double.infinity

        if dB >= triggerDB {
            sustainedMs += durationMs
            if sustainedMs >= Double(triggerSustainMs) {
                sustainedMs = 0
                cooldownRemainingMs = Double(cooldownMs)
                return true
            }
        } else {
            // Leak the accumulator gently so a brief gap mid-burst
            // doesn't reset to zero — natural speech has short
            // intra-syllable dips that shouldn't disqualify a trigger.
            sustainedMs = max(0, sustainedMs - durationMs * 0.5)
        }
        return false
    }
}

/// PreRollBuffer keeps the last `capacityMs` of audio so we can send
/// it into Grok the moment a session opens. Without this, the user's
/// first 100–300 ms (the part that triggered the VAD) would be
/// chopped off because the WS handshake takes that long.
public struct PreRollBuffer {
    public let capacityBytes: Int
    private var buffer: Data = Data()
    /// When sealed, `append` stops evicting — the buffer grows
    /// unbounded. VoiceLoop seals the moment the VAD fires so that
    /// *everything* from the trigger until the Grok session is ready
    /// (the WS handshake can take 1s+) is preserved, not just the last
    /// `capacityMs`. Before sealing, the ring keeps only the recent
    /// lead-in so the syllable that fired the VAD survives.
    private var sealed = false

    public init(capacityMs: Int = 500, sampleRate: Double = 24000) {
        self.capacityBytes = Int(Double(capacityMs) / 1000 * sampleRate) * MemoryLayout<Int16>.size
    }

    public mutating func append(_ pcm: Data) {
        buffer.append(pcm)
        if !sealed && buffer.count > capacityBytes {
            buffer.removeFirst(buffer.count - capacityBytes)
        }
    }

    /// Stop evicting: capture all audio from now until `drain`.
    public mutating func seal() { sealed = true }

    /// Return the buffered audio and reset to the pre-trigger ring
    /// behaviour for the next episode.
    public mutating func drain() -> Data {
        let out = buffer
        buffer = Data()
        sealed = false
        return out
    }
}
