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
Wait+Screen />.*Retire legacy cloud output flags/
Sleep 1200ms

Down
Sleep 800ms
Type "h"
Sleep 500ms
Type "h"
Wait+Screen /▸ mona\/hyper/
Sleep 1s
Type "l"
Wait+Screen />.*Keep selection stable after refresh/
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
Wait+Screen />.*Add a rate-limit detail view/
Sleep 1200ms

Tab
Wait+Screen /My Issues \(4\)/
Sleep 1200ms
Down
Sleep 600ms
Down
Sleep 600ms
Down
Wait+Screen />.*Support configurable refresh intervals/
Sleep 1500ms

Hide
Type "q"
Sleep 500ms
