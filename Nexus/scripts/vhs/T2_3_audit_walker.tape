Output replaced.gif
Set FontSize 14
Set Width 1600
Set Height 1000
Set Theme "Dracula"
Set Shell "cmd"
Set TypingSpeed 50ms

Env NEXUS_API_URL=http://localhost:8080
Env NEXUS_ADMIN_TOKEN

Type "D:\bubblefish\nexus\nexus.exe tui"
Enter
Sleep 4s
Type "4"
Sleep 3s
Screenshot audit_first.png
Type "l"
Sleep 1s
Type "l"
Sleep 1s
Screenshot audit_after_nav.png
Type "q"
