import QtQuick
import qs.Commons
import "Model.js" as Model

// Fan curve: temperature along the bottom, fan speed up the side.
// Points are dragged in place; the curve is a monotone cubic Bezier through them.
Item {
  id: editor

  property var points: []
  property real currentTemp: 0
  property bool interactive: true
  property color foreground: Color.foreground
  property color surface: Color.background
  property string fontFamily: Style.font.family
  property int activeIndex: -1

  signal pointsEdited(var points)

  readonly property color muted: Qt.darker(foreground, 1.4)
  readonly property real axisWidth: Style.space(34)
  readonly property real axisHeight: Style.space(18)
  readonly property real topPad: Style.space(10)
  readonly property real rightPad: Style.space(8)
  readonly property real plotX: axisWidth
  readonly property real plotY: topPad
  readonly property real plotW: Math.max(1, width - axisWidth - rightPad)
  readonly property real plotH: Math.max(1, height - topPad - axisHeight)
  readonly property real currentFan: Model.evaluate(points, currentTemp)

  implicitHeight: Style.space(196)

  function xFor(t) { return plotX + (t - Model.tempMin) / (Model.tempMax - Model.tempMin) * plotW }
  function yFor(f) { return plotY + plotH - (f - Model.fanMin) / (Model.fanMax - Model.fanMin) * plotH }
  function tFor(x) { return Model.tempMin + (x - plotX) / plotW * (Model.tempMax - Model.tempMin) }
  function fFor(y) { return Model.fanMin + (plotY + plotH - y) / plotH * (Model.fanMax - Model.fanMin) }
  function tint(a) { return Qt.rgba(foreground.r, foreground.g, foreground.b, a) }

  onPointsChanged: plot.requestPaint()
  onCurrentTempChanged: plot.requestPaint()
  onWidthChanged: plot.requestPaint()
  onHeightChanged: plot.requestPaint()
  onForegroundChanged: plot.requestPaint()
  onInteractiveChanged: plot.requestPaint()

  Canvas {
    id: plot
    anchors.fill: parent
    antialiasing: true

    onPaint: {
      var ctx = getContext("2d")
      ctx.reset()
      var pts = editor.points
      if (!pts || pts.length === 0) return

      // Grid: quiet guides at every quarter of fan speed and every 20 degrees.
      ctx.lineWidth = 1
      ctx.strokeStyle = editor.tint(0.07)
      for (var f = 25; f <= 100; f += 25) {
        var gy = Math.round(editor.yFor(f)) + 0.5
        ctx.beginPath(); ctx.moveTo(editor.plotX, gy); ctx.lineTo(editor.plotX + editor.plotW, gy); ctx.stroke()
      }
      for (var t = 50; t < Model.tempMax; t += 20) {
        var gx = Math.round(editor.xFor(t)) + 0.5
        ctx.beginPath(); ctx.moveTo(gx, editor.plotY); ctx.lineTo(gx, editor.plotY + editor.plotH); ctx.stroke()
      }
      ctx.strokeStyle = editor.tint(0.18)
      var base = Math.round(editor.plotY + editor.plotH) + 0.5
      ctx.beginPath(); ctx.moveTo(editor.plotX, base); ctx.lineTo(editor.plotX + editor.plotW, base); ctx.stroke()

      // Curve path, flat before the first point and after the last.
      var segs = Model.segments(pts)
      function trace() {
        ctx.moveTo(editor.plotX, editor.yFor(pts[0].f))
        ctx.lineTo(editor.xFor(pts[0].t), editor.yFor(pts[0].f))
        for (var i = 0; i < segs.length; i++) {
          var s = segs[i]
          ctx.bezierCurveTo(editor.xFor(s.c1.t), editor.yFor(s.c1.f),
                            editor.xFor(s.c2.t), editor.yFor(s.c2.f),
                            editor.xFor(s.p1.t), editor.yFor(s.p1.f))
        }
        ctx.lineTo(editor.plotX + editor.plotW, editor.yFor(pts[pts.length - 1].f))
      }

      var fill = ctx.createLinearGradient(0, editor.plotY, 0, editor.plotY + editor.plotH)
      fill.addColorStop(0, editor.tint(editor.interactive ? 0.16 : 0.08))
      fill.addColorStop(1, editor.tint(0))
      ctx.beginPath(); trace()
      ctx.lineTo(editor.plotX + editor.plotW, editor.plotY + editor.plotH)
      ctx.lineTo(editor.plotX, editor.plotY + editor.plotH)
      ctx.closePath()
      ctx.fillStyle = fill
      ctx.fill()

      ctx.beginPath(); trace()
      ctx.lineWidth = 2
      ctx.lineJoin = "round"
      ctx.lineCap = "round"
      ctx.strokeStyle = editor.tint(editor.interactive ? 1 : 0.5)
      ctx.stroke()

      // Live temperature: a dashed guide down to the axis.
      var cx = Math.round(editor.xFor(Model.clamp(editor.currentTemp, Model.tempMin, Model.tempMax))) + 0.5
      var cy = editor.yFor(editor.currentFan)
      ctx.lineWidth = 1
      ctx.strokeStyle = editor.tint(0.35)
      ctx.beginPath()
      for (var y = cy + 6; y < editor.plotY + editor.plotH; y += 6) {
        ctx.moveTo(cx, y); ctx.lineTo(cx, Math.min(y + 3, editor.plotY + editor.plotH))
      }
      ctx.stroke()
    }
  }

  // Axis labels.
  Repeater {
    model: [0, 50, 100]
    Text {
      required property var modelData
      x: 0
      width: editor.axisWidth - Style.space(8)
      y: editor.yFor(modelData) - height / 2
      horizontalAlignment: Text.AlignRight
      text: modelData + "%"
      color: editor.muted
      font.family: editor.fontFamily
      font.pixelSize: Style.font.caption
    }
  }

  Repeater {
    model: [30, 50, 70, 90]
    Text {
      required property var modelData
      x: Math.max(editor.plotX, editor.xFor(modelData) - width / 2)
      y: editor.plotY + editor.plotH + Style.space(5)
      text: modelData + "°"
      color: editor.muted
      font.family: editor.fontFamily
      font.pixelSize: Style.font.caption
    }
  }

  // Where the fan is right now.
  Item {
    id: liveDot
    x: editor.xFor(Model.clamp(editor.currentTemp, Model.tempMin, Model.tempMax)) - width / 2
    y: editor.yFor(editor.currentFan) - height / 2
    width: Style.space(8)
    height: width
    Behavior on x { NumberAnimation { duration: 600; easing.type: Easing.OutCubic } }
    Behavior on y { NumberAnimation { duration: 600; easing.type: Easing.OutCubic } }

    Rectangle {
      anchors.centerIn: parent
      width: parent.width * 2.6
      height: width
      radius: width / 2
      color: editor.tint(0.18)
      SequentialAnimation on scale {
        loops: Animation.Infinite
        running: editor.visible
        NumberAnimation { from: 0.55; to: 1; duration: 1100; easing.type: Easing.OutCubic }
        NumberAnimation { from: 1; to: 0.55; duration: 900; easing.type: Easing.InOutSine }
      }
    }
    Rectangle {
      anchors.fill: parent
      radius: width / 2
      color: editor.foreground
    }
  }

  // Draggable control points.
  Repeater {
    model: editor.points.length

    Item {
      id: handle
      required property int index
      readonly property var point: editor.points[index] || { t: 0, f: 0 }
      readonly property bool hot: dragArea.containsMouse || dragArea.pressed || editor.activeIndex === index

      width: Style.space(26)
      height: width
      x: editor.xFor(point.t) - width / 2
      y: editor.yFor(point.f) - height / 2
      z: hot ? 3 : 2

      Rectangle {
        anchors.centerIn: parent
        width: Style.space(11)
        height: width
        radius: width / 2
        color: handle.hot ? editor.foreground : editor.surface
        border.width: 2
        border.color: editor.interactive ? editor.foreground : editor.tint(0.5)
        scale: handle.hot ? 1.25 : 1
        Behavior on scale { NumberAnimation { duration: 140; easing.type: Easing.OutCubic } }
        Behavior on color { ColorAnimation { duration: 140 } }
      }

      // Value chip while hovering or dragging.
      Rectangle {
        visible: handle.hot && editor.interactive
        anchors.horizontalCenter: parent.horizontalCenter
        anchors.bottom: parent.top
        anchors.bottomMargin: Style.space(2)
        width: chipText.implicitWidth + Style.space(12)
        height: chipText.implicitHeight + Style.space(6)
        radius: Style.cornerRadius > 0 ? height / 2 : 0
        color: Color.tooltip.background
        border.width: 1
        border.color: Color.tooltip.border
        Text {
          id: chipText
          anchors.centerIn: parent
          text: handle.point.t + "° · " + handle.point.f + "%"
          color: Color.tooltip.text
          font.family: editor.fontFamily
          font.pixelSize: Style.font.caption
        }
      }

      MouseArea {
        id: dragArea
        anchors.fill: parent
        enabled: editor.interactive
        hoverEnabled: true
        preventStealing: true
        cursorShape: pressed ? Qt.ClosedHandCursor : Qt.OpenHandCursor
        onPositionChanged: function(mouse) {
          if (!pressed) return
          var p = mapToItem(editor, mouse.x, mouse.y)
          editor.pointsEdited(Model.constrain(editor.points, handle.index, editor.tFor(p.x), editor.fFor(p.y)))
        }
      }
    }
  }
}
