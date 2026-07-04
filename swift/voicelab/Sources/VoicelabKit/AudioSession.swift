// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Platform-split audio-session configuration. iOS requires an
// AVAudioSession to be activated with the right category before the
// engine's input node will deliver mic audio, and the same category
// (.playAndRecord + .voiceChat) is what lets the app keep capturing
// and playing while backgrounded or screen-locked — the car-mounted
// deployment's core requirement. macOS has no AVAudioSession, so the
// call is a no-op there.

import Foundation

#if os(iOS)
import AVFAudio

enum AudioSessionConfig {
    /// Activate a voice-chat play-and-record session. `.voiceChat`
    /// mode composes with the input node's VoiceProcessingIO to give
    /// AEC + NS + AGC. `.defaultToSpeaker` keeps output on the
    /// built-in speaker rather than the earpiece; `.allowBluetooth`
    /// (HFP) and `.allowBluetoothA2DP` let a paired car system carry
    /// the audio. `UIBackgroundModes: audio` in Info.plist plus this
    /// active session is what survives lock/background.
    static func activateVoiceChat() throws {
        let session = AVAudioSession.sharedInstance()
        try session.setCategory(
            .playAndRecord,
            mode: .voiceChat,
            options: [.defaultToSpeaker, .allowBluetooth, .allowBluetoothA2DP]
        )
        try session.setActive(true)
    }
}
#else
enum AudioSessionConfig {
    static func activateVoiceChat() throws {}
}
#endif
