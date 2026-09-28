import QtQuick
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui
import qs.Ui as Ui
import "Model.js" as Model

Panel {
  id: app
  moduleName: "local.omafan"
  ipcTarget: "local.omafan"
  manageIpc: false

  // Last `omafan status --json`, or null before the first reply.
  property var snapshot: null
  property string lastError: ""
  property string actionError: ""
  property int selectedIndex: 0
  property bool cursorActive: false
  // Optimistic state while a command runs, so the UI answers the click at once.
  property int desiredEnabled: -1
  property string desiredProfile: ""
  // Custom curve being dragged or saved; null once the daemon has it.
  property var draft: null
  property string sentDraft: ""

  readonly property bool connected: snapshot !== null && lastError === ""
  readonly property bool busy: actionProc.running
  readonly property bool controlEnabled: desiredEnabled >= 0 ? desiredEnabled === 1 : (snapshot ? snapshot.enabled === true : false)
  readonly property string profile: draft ? "custom" : (desiredProfile !== "" ? desiredProfile : (snapshot ? String(snapshot.profile || "balanced") : "balanced"))
  readonly property var savedCustom: snapshot && Model.validPoints(snapshot.custom) ? snapshot.custom : Model.presets.balanced
  readonly property var points: {
    if (draft) return draft
    if (profile === "custom") return savedCustom
    if (snapshot && snapshot.presets && Model.validPoints(snapshot.presets[profile])) return snapshot.presets[profile]
    return Model.presets[profile] || Model.presets.balanced
  }
  readonly property string controller: snapshot ? String(snapshot.controller || "firmware") : "firmware"
  readonly property bool driving: controller === "omafan" || controller === "emergency"

  readonly property real cpuTemp: num(snapshot ? snapshot.cpu : null, num(snapshot ? snapshot.apu : null, 0))
  readonly property real gpuTemp: num(snapshot ? snapshot.gpu : null, 0)
  readonly property real controlTemp: num(snapshot ? snapshot.temp : null, cpuTemp)
  readonly property int fanRpm: snapshot ? Number(snapshot.fanRpm || 0) : 0
  // Duty when OmaFan drives; otherwise RPM against the EC's 2300 RPM ceiling.
  readonly property real fanPercent: snapshot && snapshot.duty !== null && snapshot.duty !== undefined
    ? Number(snapshot.duty) : Math.min(100, fanRpm / 2300 * 100)

  readonly property string fontFamily: bar ? bar.fontFamily : Style.font.family
  readonly property color foreground: bar ? bar.foreground : Color.foreground
  readonly property color muted: Qt.darker(foreground, 1.4)
  readonly property string fanGlyph: "\u{F0210}"

  function num(v, fallback) {
    return v === null || v === undefined || !isFinite(Number(v)) ? fallback : Number(v)
  }

  function stateText() {
    if (!snapshot || lastError !== "") return "Service unavailable"
    if (controller === "emergency") return "Full speed · running hot"
    if (!controlEnabled) return "Firmware auto"
    if (!driving) return "Waiting for sensors"
    return Model.profileLabel(profile) + " curve"
  }

  function refresh() {
    if (statusProc.running) return
    statusProc.running = true
  }

  function run(args) {
    if (busy) return
    actionError = ""
    actionProc.command = ["omafan"].concat(args)
    actionProc.running = true
  }

  function setEnabled(on) {
    if (!connected) return
    desiredEnabled = on ? 1 : 0
    run([on ? "enable" : "disable"])
  }

  function selectProfile(value) {
    if (!controlEnabled || !connected) return
    saveTimer.stop()
    draft = null
    desiredProfile = value
    run(["profile", value])
  }

  // Dragging any curve's point switches to a custom curve, saved shortly after
  // the last change so a drag doesn't send a command per pixel.
  function editCurve(next) {
    if (!controlEnabled || !connected) return
    draft = next
    saveTimer.restart()
  }

  function saveCurve() {
    if (!draft) return
    if (busy) { saveTimer.restart(); return }
    desiredProfile = "custom"
    sentDraft = Model.formatPoints(draft)
    run(["curve", sentDraft])
  }

  Timer {
    id: saveTimer
    interval: 250
    onTriggered: app.saveCurve()
  }

  implicitWidth: button.implicitWidth
  implicitHeight: button.implicitHeight

  Component.onCompleted: refresh()
  onOpenedChanged: if (opened) { cursorActive = false; selectedIndex = 0; refresh() }

  Timer {
    interval: Math.max(500, Number(settings.refreshIntervalMs || 1000))
    running: true
    repeat: true
    onTriggered: app.refresh()
  }

  Process {
    id: statusProc
    command: ["omafan", "status", "--json"]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: {
        var raw = String(text || "").trim()
        if (raw === "") return
        try {
          app.snapshot = JSON.parse(raw)
          app.lastError = ""
          if (!actionProc.running) { app.desiredEnabled = -1; app.desiredProfile = "" }
        } catch (error) {
          app.lastError = "Invalid reply from omafan"
        }
      }
    }
    stderr: StdioCollector {
      waitForEnd: true
      onStreamFinished: {
        var message = String(text || "").trim()
        if (message !== "") app.lastError = message.replace(/^omafan:\s*/, "")
      }
    }
  }

  Process {
    id: actionProc
    stdout: StdioCollector { waitForEnd: true }
    stderr: StdioCollector { id: actionStderr; waitForEnd: true }
    onExited: function(exitCode) {
      var wasCurve = String(actionProc.command[1]) === "curve"
      if (exitCode === 0) {
        // Keep a newer drag that arrived while this one was saving.
        if (wasCurve && app.draft && Model.formatPoints(app.draft) === app.sentDraft && !saveTimer.running) app.draft = null
      } else {
        if (wasCurve) app.draft = null
        app.desiredEnabled = -1
        app.desiredProfile = ""
        app.actionError = String(actionStderr.text || "").trim().replace(/^omafan:\s*/, "") || "Couldn't change the fan settings."
      }
      app.refresh()
    }
  }

  IpcHandler {
    target: app.ipcTarget
    function open(): void { app.open() }
    function close(): void { app.close() }
    function toggle(): void { app.toggle() }
    function refresh(): string { app.refresh(); return "ok" }
  }

  BarIconButton {
    id: button
    anchors.fill: parent
    bar: app.bar
    text: app.fanGlyph
    fontFamily: "JetBrainsMono Nerd Font"
    active: app.driving
    tooltipText: app.snapshot
      ? "OmaFan · " + Model.formatRpm(app.fanRpm) + " RPM · " + Math.round(app.controlTemp) + "°"
      : "OmaFan · service unavailable"
    onPressed: function(mouseButton) {
      if (mouseButton === Qt.MiddleButton) app.refresh()
      else app.toggle()
    }
  }

  // Cursor order: toggle, then the four profiles.
  readonly property int cursorCount: 5

  function moveCursor(delta) {
    cursorActive = true
    selectedIndex = Math.max(0, Math.min(cursorCount - 1, selectedIndex + delta))
  }

  function activateCursor() {
    if (selectedIndex === 0) setEnabled(!controlEnabled)
    else selectProfile(Model.profiles[selectedIndex - 1].value)
  }

  KeyboardPanel {
    id: panel
    anchorItem: button
    owner: app
    bar: app.bar
    open: app.opened
    focusTarget: keyCatcher
    contentWidth: panel.fittedContentWidth(Style.space(400))
    contentHeight: panel.fittedContentHeight(content.implicitHeight)

    PanelKeyCatcher {
      id: keyCatcher
      anchors.fill: parent
      onMoveRequested: function(dx, dy) { app.moveCursor(dy || dx) }
      onTabRequested: function(direction) { app.moveCursor(direction) }
      onActivateRequested: app.activateCursor()
      onCloseRequested: app.close()

      Column {
        id: content
        width: parent.width
        spacing: Style.space(14)

        PanelHero {
          width: parent.width
          title: "OmaFan"
          meta: app.stateText()
          foreground: app.foreground
          fontFamily: app.fontFamily
          iconOpacity: app.driving ? 1 : 0.5
          iconComponent: Component {
            Text {
              text: app.fanGlyph
              color: app.foreground
              font.family: app.fontFamily
              font.pixelSize: Style.font.display
              // Spins at a pace that follows the fan.
              RotationAnimator on rotation {
                from: 0
                to: 360
                loops: Animation.Infinite
                duration: Math.round(2400 / Math.max(0.15, app.fanRpm / 2300))
                running: app.opened && app.fanRpm > 0
              }
            }
          }
          trailingControl: Component {
            ToggleSwitch {
              id: controlSwitch
              checked: app.controlEnabled
              busy: app.busy && app.desiredEnabled >= 0
              interactive: app.connected
              foreground: app.foreground
              hasCursor: app.cursorActive && app.selectedIndex === 0
              onHovered: function(on) { if (on) { app.cursorActive = true; app.selectedIndex = 0 } }
              onToggled: app.setEnabled(!app.controlEnabled)
              PanelToolTip {
                visible: controlSwitch.containsMouse
                text: app.controlEnabled ? "OmaFan is driving the fans · turn off for firmware auto" : "Firmware is driving the fans · turn on to use a curve"
                fontFamily: app.fontFamily
              }
            }
          }
        }

        // ---------- Live readings ----------
        Row {
          width: parent.width
          spacing: Style.space(12)
          opacity: app.snapshot ? 1 : 0.45
          readonly property real cell: (width - spacing * 2) / 3

          StatCell { width: parent.cell; label: "CPU"; value: app.snapshot ? Math.round(app.cpuTemp) : "—"; unit: "°C"; fraction: app.cpuTemp / 100 }
          StatCell { width: parent.cell; label: "GPU"; value: app.snapshot && app.snapshot.gpu !== null ? Math.round(app.gpuTemp) : "—"; unit: "°C"; fraction: app.gpuTemp / 100 }
          StatCell { width: parent.cell; label: "Fan"; value: Model.formatRpm(app.fanRpm); unit: "RPM"; fraction: app.fanPercent / 100 }
        }

        PanelSeparator { foreground: app.foreground }

        // ---------- Profile ----------
        Column {
          width: parent.width
          spacing: Style.space(10)
          opacity: app.controlEnabled && app.connected ? 1 : 0.45
          Behavior on opacity { NumberAnimation { duration: 160 } }

          PanelSectionHeader { text: "FAN PROFILE"; foreground: app.foreground; fontFamily: app.fontFamily }

          Row {
            id: profileRow
            width: parent.width
            spacing: Style.space(6)
            readonly property real cellWidth: (width - spacing * (Model.profiles.length - 1)) / Model.profiles.length

            Repeater {
              model: Model.profiles
              Ui.Button {
                required property var modelData
                required property int index
                width: profileRow.cellWidth
                iconText: modelData.icon
                iconSize: Style.font.title
                text: modelData.label
                fontSize: Style.font.bodySmall
                foreground: app.foreground
                fontFamily: app.fontFamily
                horizontalPadding: Style.spacing.controlPaddingX
                verticalPadding: Style.spacing.controlPaddingY + Style.space(2)
                bordered: true
                enabled: app.controlEnabled && app.connected && !app.busy
                active: app.profile === modelData.value
                hasCursor: app.cursorActive && app.selectedIndex === index + 1
                onClicked: app.selectProfile(modelData.value)
                onHovered: function(h) { if (h) { app.cursorActive = true; app.selectedIndex = index + 1 } }
              }
            }
          }
        }

        // ---------- Curve ----------
        Column {
          width: parent.width
          spacing: Style.space(8)
          opacity: app.controlEnabled && app.connected ? 1 : 0.45
          Behavior on opacity { NumberAnimation { duration: 160 } }

          Item {
            width: parent.width
            implicitHeight: curveHeader.implicitHeight
            PanelSectionHeader {
              id: curveHeader
              text: "FAN CURVE"
              foreground: app.foreground
              fontFamily: app.fontFamily
            }
            Text {
              anchors.right: parent.right
              anchors.verticalCenter: curveHeader.verticalCenter
              text: app.driving
                ? Math.round(app.controlTemp) + "°  →  " + Math.round(app.fanPercent) + "%"
                : Math.round(app.controlTemp) + "°  ·  firmware"
              color: app.muted
              font.family: app.fontFamily
              font.pixelSize: Style.font.caption
            }
          }

          CurveEditor {
            width: parent.width
            points: app.points
            currentTemp: app.controlTemp
            interactive: app.controlEnabled && app.connected
            foreground: app.foreground
            fontFamily: app.fontFamily
            onPointsEdited: function(next) { app.editCurve(next) }
          }

        }

        Text {
          visible: text !== ""
          width: parent.width
          wrapMode: Text.Wrap
          textFormat: Text.PlainText
          text: app.actionError || app.lastError || (app.snapshot ? String(app.snapshot.error || "") : "")
          color: app.bar ? app.bar.urgent : Color.urgent
          font.family: app.fontFamily
          font.pixelSize: Style.font.caption
        }
      }
    }
  }

  component StatCell: Column {
    id: stat
    property string label: ""
    property var value: ""
    property string unit: ""
    property real fraction: 0
    spacing: Style.space(4)

    Text {
      text: stat.label.toUpperCase()
      color: app.muted
      font.family: app.fontFamily
      font.pixelSize: Style.font.caption
      font.bold: true
      font.letterSpacing: 1.2
    }

    Row {
      spacing: Style.space(3)
      Text {
        id: statValue
        text: String(stat.value)
        color: app.foreground
        font.family: app.fontFamily
        font.pixelSize: Style.font.display
        font.bold: true
      }
      Text {
        anchors.baseline: statValue.baseline
        text: stat.unit
        color: app.muted
        font.family: app.fontFamily
        font.pixelSize: Style.font.caption
      }
    }

    Rectangle {
      width: parent.width
      height: Math.max(Style.space(3), Math.round(Style.spacing.controlHeight * 0.1))
      radius: height / 2
      color: Style.selectedFillFor(app.foreground, Color.accent)
      Rectangle {
        height: parent.height
        radius: parent.radius
        width: parent.width * Model.clamp(stat.fraction, 0, 1)
        color: app.foreground
        Behavior on width { NumberAnimation { duration: 600; easing.type: Easing.OutCubic } }
      }
    }
  }
}
