Output docs/demo.gif

Require hyper

Set Shell "bash"
Set FontFamily "JetBrains Mono"
Set FontSize 16
Set Width 1200
Set Height 450
Set TypingSpeed 40ms

Env HYPER_DEMO "1"
Env TZ "UTC"

Hide
Type "hyper"
Enter
Wait+Screen /refreshed 3:04PM/
Show
Sleep 1500ms

Type "E"
Wait+Screen /marked done/
Sleep 800ms

Down
Sleep 500ms
Type "E"
Wait+Screen /marked done/
Sleep 800ms

Down
Sleep 500ms
Type "E"
Wait+Screen /marked done/
Sleep 800ms

Down
Sleep 500ms
Type "E"
Wait+Screen /marked done/
Sleep 800ms

Down
Wait+Screen />.*Retire legacy/
Sleep 1200ms

Down
Sleep 800ms
Type "h"
Sleep 500ms
Type "h"
Wait+Screen /▸ mona\/hyper/
Sleep 1s
Type "l"
Wait+Screen />.*Keep selection/
Sleep 1200ms

Tab
Wait+Screen /My Pull Requests \(5\)/
Sleep 1200ms
Down
Sleep 600ms
Down
Sleep 600ms
Down
Sleep 600ms
Down
Wait+Screen />.*Add a rate/
Sleep 1200ms

Tab
Wait+Screen /My Issues/
Sleep 1200ms
Down
Sleep 600ms
Down
Sleep 600ms
Down
Wait+Screen />.*Support config/
Sleep 1500ms

Hide
Type "q"
Sleep 500ms
