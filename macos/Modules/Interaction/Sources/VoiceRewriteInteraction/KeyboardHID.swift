#if os(macOS)
import AppKit
import CoreGraphics
import Foundation
import IOKit.hid

// IOKit's HID API expects a CFString, while modern Swift SDKs expose the
// CoreFoundation run-loop mode constant through a distinct CFRunLoopMode type.
private let hidCommonRunLoopMode: CFString = CFStringCreateWithCString(
    kCFAllocatorDefault, "kCFRunLoopCommonModes", CFStringBuiltInEncodings.UTF8.rawValue)!

/// An actual HID keyboard, not a virtual "any keyboard" shortcut.
public struct MacKeyboardDevice: Identifiable, Equatable, Sendable {
    public let id: String
    public let name: String
    public let builtIn: Bool
    public let selectable: Bool

    public var title: String {
        (builtIn ? "内建键盘 · " : "外接键盘 · ") + name +
            (selectable ? "" : "（无法与同型号设备区分）")
    }

    public init(id: String, name: String, builtIn: Bool, selectable: Bool = true) {
        self.id = id
        self.name = name
        self.builtIn = builtIn
        self.selectable = selectable
    }

    /// A stable identifier where HID exposes a serial or transport location.
    /// Never use the transient IOHIDDevice pointer for saved preferences.
    public static func identity(vendor: Int, product: Int, location: Int,
                                serial: String, builtIn: Bool) -> String? {
        if builtIn { return "builtin:\(vendor):\(product):\(location)" }
        if !serial.isEmpty { return "serial:\(vendor):\(product):\(serial)" }
        if location != 0 { return "port:\(vendor):\(product):\(location)" }
        return nil // indistinguishable external devices cannot be targeted safely
    }
}

public struct MacKeyboardHIDBinding: Equatable, Sendable {
    public let deviceID: String
    public let key: MacModifierKey

    public init(deviceID: String, key: MacModifierKey) {
        self.deviceID = deviceID
        self.key = key
    }
}

/// HID usage IDs of left/right Ctrl, Option (Alt), and Command (GUI).
/// Values come from USB HID keyboard page 0x07, not layout-dependent keycodes.
public enum MacKeyboardHIDKey {
    public static func modifier(usage: Int) -> MacModifierKey? {
        switch usage {
        case 0xE0: return .leftControl
        case 0xE4: return .rightControl
        case 0xE2: return .leftOption
        case 0xE6: return .rightOption
        case 0xE3: return .leftCommand
        case 0xE7: return .rightCommand
        default: return nil
        }
    }
}

private let hidDeviceAdded: IOHIDDeviceCallback = { context, _, _, device in
    guard let context else { return }
    let object = Unmanaged<MacKeyboardHIDMonitor>.fromOpaque(context).takeUnretainedValue()
    MainActor.assumeIsolated { object.add(device) }
}
private let hidDeviceRemoved: IOHIDDeviceCallback = { context, _, _, device in
    guard let context else { return }
    let object = Unmanaged<MacKeyboardHIDMonitor>.fromOpaque(context).takeUnretainedValue()
    MainActor.assumeIsolated { object.remove(device) }
}
private let hidInputChanged: IOHIDValueCallback = { context, _, _, value in
    guard let context else { return }
    let object = Unmanaged<MacKeyboardHIDMonitor>.fromOpaque(context).takeUnretainedValue()
    MainActor.assumeIsolated { object.handle(value) }
}

/// Device-specific single-modifier holds; never installs an exclusive HID grab.
/// In a scoped mode the Quartz/global modifier binding must be disabled so a
/// key from another keyboard cannot silently trigger the same action.
@MainActor
public final class MacKeyboardHIDMonitor {
    private struct DeviceRecord {
        let device: IOHIDDevice
        let descriptor: MacKeyboardDevice
    }

    private var manager: IOHIDManager?
    private var records: [Int: DeviceRecord] = [:]
    private var pressed: Set<String> = []
    private var heldDevice: String?
    private var state = MacModifierHoldState()
    private var dictation: MacKeyboardHIDBinding?
    private var rewrite: MacKeyboardHIDBinding?
    private var paused = false
    private var suspended = false
    private var learnDeviceID: String?
    private var learn: (@MainActor (MacModifierKey) -> Void)?
    private let threshold: TimeInterval
    private let onDevices: @MainActor ([MacKeyboardDevice]) -> Void
    private let onPress: @MainActor (MacModifierKey, String, Int32?, TimeInterval) -> Void
    private let onRelease: @MainActor (MacModifierKey, String) -> Void
    private let onCancel: @MainActor () -> Void

    public init(threshold: TimeInterval = SideHold.threshold,
                onDevices: @escaping @MainActor ([MacKeyboardDevice]) -> Void,
                onPress: @escaping @MainActor (MacModifierKey, String, Int32?, TimeInterval) -> Void,
                onRelease: @escaping @MainActor (MacModifierKey, String) -> Void,
                onCancel: @escaping @MainActor () -> Void) {
        self.threshold = threshold
        self.onDevices = onDevices
        self.onPress = onPress
        self.onRelease = onRelease
        self.onCancel = onCancel
    }

    public var devices: [MacKeyboardDevice] {
        let groups = Dictionary(grouping: records.values.map(\.descriptor), by: \.id)
        // Distinct SwiftUI identities: duplicate HID identities are one disabled
        // choice rather than two indistinguishable ForEach rows.
        return groups.compactMap { id, entries -> MacKeyboardDevice? in
            guard let first = entries.first else { return nil }
            return MacKeyboardDevice(id: id, name: first.name, builtIn: first.builtIn,
                                     selectable: first.selectable && entries.count == 1)
        }.sorted { $0.title.localizedStandardCompare($1.title) == .orderedAscending }
    }

    public func start() throws {
        if manager != nil { return }
        let newManager = IOHIDManagerCreate(kCFAllocatorDefault, 0)
        // Match keyboard collections (HID Generic Desktop / Keyboard).
        IOHIDManagerSetDeviceMatching(newManager, [
            "DeviceUsagePage": 1, "DeviceUsage": 6
        ] as CFDictionary)
        let ptr = Unmanaged.passUnretained(self).toOpaque()
        IOHIDManagerRegisterDeviceMatchingCallback(newManager, hidDeviceAdded, ptr)
        IOHIDManagerRegisterDeviceRemovalCallback(newManager, hidDeviceRemoved, ptr)
        IOHIDManagerRegisterInputValueCallback(newManager, hidInputChanged, ptr)
        IOHIDManagerScheduleWithRunLoop(newManager, CFRunLoopGetMain(), hidCommonRunLoopMode)
        let result = IOHIDManagerOpen(newManager, 0)
        guard result == kIOReturnSuccess else {
            IOHIDManagerUnscheduleFromRunLoop(newManager, CFRunLoopGetMain(), CFRunLoopMode.commonModes.rawValue)
            throw NSError(domain: "VoiceRewrite.KeyboardHID", code: Int(result),
                          userInfo: [NSLocalizedDescriptionKey:
                            "无法监听键盘来源，请授予输入监控权限后重启 VoiceRewrite"])
        }
        manager = newManager
        publish()
    }

    public func stop() {
        cancel()
        learn = nil
        learnDeviceID = nil
        if let manager {
            IOHIDManagerClose(manager, 0)
            IOHIDManagerUnscheduleFromRunLoop(manager, CFRunLoopGetMain(), CFRunLoopMode.commonModes.rawValue)
        }
        manager = nil
        records.removeAll()
        pressed.removeAll()
        publish()
    }

    public func setBindings(dictation: MacKeyboardHIDBinding?, rewrite: MacKeyboardHIDBinding?) {
        cancel()
        self.dictation = dictation
        self.rewrite = rewrite
    }

    public func setSuspended(_ value: Bool) {
        suspended = value
        if value { cancel() }
    }

    public func setPaused(_ value: Bool) {
        paused = value
        if value { cancel() }
    }

    public func capture(deviceID: String?, onKey: (@MainActor (MacModifierKey) -> Void)?) {
        cancel()
        learnDeviceID = deviceID
        learn = onKey
    }

    public func refresh() {
        publish()
    }

    private func address(_ device: IOHIDDevice) -> Int {
        Int(bitPattern: Unmanaged.passUnretained(device).toOpaque())
    }

    private func property(_ device: IOHIDDevice, _ key: String) -> Any? {
        IOHIDDeviceGetProperty(device, key as CFString)
    }

    private func number(_ device: IOHIDDevice, _ key: String) -> Int {
        (property(device, key) as? NSNumber)?.intValue ?? 0
    }

    fileprivate func add(_ device: IOHIDDevice) {
        let vendor = number(device, "VendorID")
        let product = number(device, "ProductID")
        let location = number(device, "LocationID")
        let serial = property(device, "SerialNumber") as? String ?? ""
        let transport = (property(device, "Transport") as? String ?? "").lowercased()
        let builtIn = number(device, "Built-In") != 0 ||
            transport.contains("internal") || transport.contains("spi")
        let id = MacKeyboardDevice.identity(vendor: vendor, product: product, location: location,
                                             serial: serial, builtIn: builtIn)
        let name = property(device, "Product") as? String ?? "Keyboard"
        // Unknown/indistinguishable keyboards are shown but cannot be bound.
        let descriptor = MacKeyboardDevice(
            id: id ?? "unidentified:\(address(device))",
            name: name, builtIn: builtIn, selectable: id != nil)
        records[address(device)] = DeviceRecord(device: device, descriptor: descriptor)
        publish()
    }

    fileprivate func remove(_ device: IOHIDDevice) {
        let address = address(device)
        guard let removed = records.removeValue(forKey: address) else { return }
        pressed = pressed.filter { !$0.hasPrefix("\(address):") }
        if heldDevice == removed.descriptor.id { cancel() }
        publish()
    }

    fileprivate func handle(_ value: IOHIDValue) {
        let device = IOHIDElementGetDevice(IOHIDValueGetElement(value))
        guard let descriptor = records[address(device)]?.descriptor else { return }
        let element = IOHIDValueGetElement(value)
        guard IOHIDElementGetUsagePage(element) == 0x07 else { return }
        let usage = Int(IOHIDElementGetUsage(element))
        let down = IOHIDValueGetIntegerValue(value) != 0
        let fingerprint = "\(address(device)):\(usage)"
        if down {
            guard pressed.insert(fingerprint).inserted else { return } // key repeat
        } else {
            guard pressed.remove(fingerprint) != nil else { return }
        }
        guard let key = MacKeyboardHIDKey.modifier(usage: usage) else {
            if down { cancel() } // another character, including on another keyboard
            return
        }
        // Settings suspends voice recording but must still receive physical
        // HID key learning from the selected device.
        if down, let learn, let learnDeviceID,
           descriptor.id == learnDeviceID, devices.contains(where: { $0.id == descriptor.id && $0.selectable }),
           pressed.count == 1 {
            learn(key)
            return
        }
        guard !suspended, !paused else { return }
        if !down, heldDevice == descriptor.id, state.held == key {
            if let released = state.release(key) { onRelease(released, descriptor.id) }
            heldDevice = nil
            return
        }
        guard down else { return }
        // Any other modifier/chord invalidates the existing hold.
        if state.held != nil { cancel(); return }
        guard !suspended && !paused,
              devices.contains(where: { $0.id == descriptor.id && $0.selectable }),
              pressed.count == 1,
              (dictation == MacKeyboardHIDBinding(deviceID: descriptor.id, key: key) ||
               rewrite == MacKeyboardHIDBinding(deviceID: descriptor.id, key: key)),
              let token = state.press(key, unmodified: true) else { return }
        heldDevice = descriptor.id
        // Start the clock on HID input, before the modifier hold timer.
        let physicalEventReceivedAt = ProcessInfo.processInfo.systemUptime
        let target = NSWorkspace.shared.frontmostApplication?.processIdentifier
        DispatchQueue.main.asyncAfter(deadline: .now() + threshold) { [weak self] in
            guard let self, !self.paused, !self.suspended,
                  self.heldDevice == descriptor.id, self.pressed.count == 1,
                  self.state.activate(key, token: token) else { return }
            self.onPress(key, descriptor.id, target, physicalEventReceivedAt)
        }
    }

    private func cancel() {
        if state.clear() != nil { onCancel() }
        heldDevice = nil
    }

    private func publish() {
        onDevices(devices)
    }

    deinit { MainActor.assumeIsolated { stop() } }
}
#endif
