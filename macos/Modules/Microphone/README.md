# macOS Microphone

Native macOS microphone capture with `AVAudioEngine` and `AVAudioConverter`. Swift Package (macOS 12+), no app, Go or UI dependencies. Captures the **system-default input device** only; selecting another device is not implemented.

## Use in the macOS host

The host application must include `NSMicrophoneUsageDescription` in its own Info.plist and grant microphone access. The integration team owns that configuration, not this package.

```swift
import Microphone

let recorder = MicrophoneRecorder() // @MainActor
let status = recorder.permissionStatus
let afterPrompt = await recorder.requestPermission()
try await recorder.start()
// Record real microphone input. recorder.rmsLevel reports a normalized level.
let pcm: Data = try await recorder.stop()
// The host encodes pcm.base64EncodedString() as transcribe.pcm_base64.
// Or call recorder.cancel() to discard the recording.
```

`start()` requests permission if it is undetermined and throws on denial, unsupported formats, missing device, and audio engine startup failure. `stop()` drains pending conversion work and returns **headerless 16,000 Hz, one-channel, signed 16-bit little-endian PCM** (32,000 bytes per second). Empty input and a second `stop()` throw. `cancel()` is idempotent and discards pending data. Device configuration changes, sleep, conversion failure, processing backlog overflow, or hitting the 60-second deadline stop the engine and surface an error on the next `stop()`. A new capture can start after acknowledging the error with `stop()`, or after `cancel()`.

Input taps copy at most two seconds of pending device audio into a bounded serial worker queue; resampling, PCM encoding and metering happen off the callback. Output PCM is capped at 1,920,000 bytes. No ASR or network access occurs in this module. Format conversion is performed by Apple's `AVAudioConverter` with continuous streaming state and final drain; an unsupported conversion fails rather than producing a fabricated recording.

## Package verification

```sh
cd macos/Modules/Microphone
swift build
swift test
```

Tests of sample-rate conversion and mono/stereo mixing use synthetic AVAudioPCMBuffer inputs and execute **only on macOS**. Pure PCM16, buffering, clipping, silence, cancellation, empty input and capacity-limit tests also execute on Linux. Neither is a hardware acceptance test. For physical acceptance on a Mac, run from an app with a microphone usage description, explicitly test permission allowed/denied, capture a real spoken sample, check PCM byte count and audibility when decoded as 16 kHz mono PCM16, unplug or switch the default device during recording, and check that stop/cancel release the microphone indicator. The host integrator is responsible for the Go bridge.
