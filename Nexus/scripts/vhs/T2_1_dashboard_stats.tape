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
Sleep 5s
Screenshot dashboard_stats.png
Sleep 3s
Screenshot dashboard_stats_t2.png
Type "q"
