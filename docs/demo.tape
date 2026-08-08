Output docs/demo.gif

Require hyper

Set Shell "bash"
Set FontFamily "JetBrains Mono"
Set FontSize 16
Set Width 1200
Set Height 600
Set TypingSpeed 40ms

Env HYPER_DEMO "1"
Env TZ "UTC"

Hide
Type "hyper"
Enter
Wait+Screen /refreshed 3:04PM/
Show
Sleep 1500ms

Hide
Type "E"
Wait+Screen /marked done/
Show
Sleep 1500ms

Hide
Type "hh"
Wait+Screen /▸ grafana/
Show
Sleep 1s

Hide
Type "l"
Wait+Screen /Add service account support/
Show
Sleep 1500ms

Hide
Tab
Down 2
Wait+Screen />.*Document per-scenario/
Show
Sleep 2s

Hide
Tab
Down
Wait+Screen />.*Expose retry counts/
Show
Sleep 2s

Hide
Type "?"
Wait+Screen /hyper help/
Show
Sleep 2500ms

Hide
Type "?"
Wait+Screen /Expose retry counts/
Show
Sleep 1500ms

Hide
Type "q"
Sleep 500ms
