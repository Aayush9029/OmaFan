import QtQuick
import QtQuick.Layouts
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

  // Design preview: telemetry is simulated and nothing touches the fans yet.
  readonly property bool demo: true

  property bool controlEnabled: true
  property string profile: "balanced"
  property var customPoints: Model.clone(Model.presets.balanced)
  property var appliedCustom: Model.clone(Model.presets.balanced)
  property real cpuTemp: 61
  property real gpuTemp: 52
  property int selectedIndex: 0
  property bool cursorActive: false

  readonly property var points: profile === "custom" ? customPoints : Model.presets[profile]
  readonly property bool dirty: JSON.stringify(customPoints) !== JSON.stringify(appliedCustom)
  readonly property real fanPercent: controlEnabled ? Model.evaluate(points, cpuTemp) : 34
  readonly property int maxRpm: 3300
  readonly property int fanRpm: Math.round(fanPercent / 100 * maxRpm)
  readonly property string fontFamily: bar ? bar.fontFamily : Style.font.family
  readonly property color foreground: bar ? bar.foreground : Color.foreground
  readonly property color muted: Qt.darker(foreground, 1.4)
  readonly property string fanGlyph: "\u{F0210}"

  function stateText() {
    if (!controlEnabled) return "Firmware auto"
    if (profile === "custom" && dirty) return "Custom · not applied"
    return Model.profileLabel(profile) + " curve"
  }

  function selectProfile(value) {
    if (!controlEnabled) return
    if (value === "custom" && profile !== "custom") customPoints = Model.clone(appliedCustom)
    profile = value
  }

  // Dragging a preset's point forks it into the custom curve.
  function editCurve(next) {
    if (!controlEnabled) return
    customPoints = next
    profile = "custom"
  }

  function applyCurve() { appliedCustom = Model.clone(customPoints) }
  function resetCurve() { customPoints = Model.clone(appliedCustom) }

  implicitWidth: button.implicitWidth
  implicitHeight: button.implicitHeight

  onOpenedChanged: if (opened) { cursorActive = false; selectedIndex = 0 }

  Timer {
    interval: 1400
    running: app.demo
    repeat: true
    onTriggered: {
      app.cpuTemp = Model.clamp(app.cpuTemp + (Math.random() - 0.48) * 4, 44, 82)
      app.gpuTemp = Model.clamp(app.cpuTemp - 9 + (Math.random() - 0.5) * 3, 38, 76)
    }
  }

  Behavior on cpuTemp { NumberAnimation { duration: 900; easing.type: Easing.OutCubic } }
  Behavior on gpuTemp { NumberAnimation { duration: 900; easing.type: Easing.OutCubic } }

  IpcHandler {
    target: app.ipcTarget
    function open(): void { app.open() }
    function close(): void { app.close() }
    function toggle(): void { app.toggle() }
    function profile(value: string): string { app.selectProfile(value); return app.profile }
  }

  BarIconButton {
    id: button
    anchors.fill: parent
    bar: app.bar
    text: app.fanGlyph
    fontFamily: "JetBrainsMono Nerd Font"
    active: app.controlEnabled
    tooltipText: "OmaFan · " + Model.formatRpm(app.fanRpm) + " RPM · " + Math.round(app.cpuTemp) + "°"
    onPressed: function(mouseButton) { app.toggle() }
  }

  // Cursor order: toggle, four profiles, reset, apply.
  readonly property int cursorCount: 7

  function moveCursor(delta) {
    cursorActive = true
    selectedIndex = Math.max(0, Math.min(cursorCount - 1, selectedIndex + delta))
  }

  function activateCursor() {
    if (selectedIndex === 0) controlEnabled = !controlEnabled
    else if (selectedIndex <= 4) selectProfile(Model.profiles[selectedIndex - 1].value)
    else if (selectedIndex === 5 && dirty) resetCurve()
    else if (selectedIndex === 6 && dirty) applyCurve()
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
          iconOpacity: app.controlEnabled ? 1 : 0.5
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
                duration: Math.round(24000 / Math.max(0.15, app.fanPercent / 100) / 10)
                running: app.opened && app.fanRpm > 0
              }
            }
          }
          trailingControl: Component {
            ToggleSwitch {
              id: controlSwitch
              checked: app.controlEnabled
              foreground: app.foreground
              hasCursor: app.cursorActive && app.selectedIndex === 0
              onHovered: function(on) { if (on) { app.cursorActive = true; app.selectedIndex = 0 } }
              onToggled: app.controlEnabled = !app.controlEnabled
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
          readonly property real cell: (width - spacing * 2) / 3

          StatCell { width: parent.cell; label: "CPU"; value: Math.round(app.cpuTemp); unit: "°C"; fraction: app.cpuTemp / 100 }
          StatCell { width: parent.cell; label: "GPU"; value: Math.round(app.gpuTemp); unit: "°C"; fraction: app.gpuTemp / 100 }
          StatCell { width: parent.cell; label: "Fan"; value: Model.formatRpm(app.fanRpm); unit: "RPM"; fraction: app.fanPercent / 100 }
        }

        PanelSeparator { foreground: app.foreground }

        // ---------- Profile ----------
        Column {
          width: parent.width
          spacing: Style.space(10)
          opacity: app.controlEnabled ? 1 : 0.45
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
                enabled: app.controlEnabled
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
          opacity: app.controlEnabled ? 1 : 0.45
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
              text: Math.round(app.cpuTemp) + "°  →  " + Math.round(app.fanPercent) + "%"
              color: app.muted
              font.family: app.fontFamily
              font.pixelSize: Style.font.caption
            }
          }

          CurveEditor {
            width: parent.width
            points: app.points
            currentTemp: app.cpuTemp
            interactive: app.controlEnabled
            foreground: app.foreground
            fontFamily: app.fontFamily
            onPointsEdited: function(next) { app.editCurve(next) }
          }

          Text {
            width: parent.width
            horizontalAlignment: Text.AlignHCenter
            text: app.profile === "custom" ? "Drag points to shape the curve" : "Drag a point to make a custom curve"
            color: app.muted
            font.family: app.fontFamily
            font.pixelSize: Style.font.caption
          }
        }

        RowLayout {
          width: parent.width
          spacing: Style.space(8)
          visible: app.controlEnabled && app.profile === "custom"
          NativeButton {
            Layout.fillWidth: true
            Layout.preferredWidth: 1
            text: "Reset"
            iconText: "\u{F0450}"
            cursorIndex: 5
            enabled: app.dirty
            onClicked: app.resetCurve()
          }
          NativeButton {
            Layout.fillWidth: true
            Layout.preferredWidth: 1
            text: "Apply curve"
            iconText: "\u{F012C}"
            cursorIndex: 6
            enabled: app.dirty
            active: app.dirty
            onClicked: app.applyCurve()
          }
        }

        Text {
          visible: app.demo
          width: parent.width
          horizontalAlignment: Text.AlignHCenter
          text: "Design preview · simulated readings"
          color: app.muted
          opacity: 0.7
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

  component NativeButton: Ui.Button {
    property int cursorIndex: -1
    foreground: app.foreground
    fontFamily: app.fontFamily
    bordered: true
    opacity: enabled ? 1 : 0.45
    hasCursor: app.cursorActive && app.selectedIndex === cursorIndex
    onHovered: function(on) { if (on) { app.cursorActive = true; app.selectedIndex = cursorIndex } }
  }
}
