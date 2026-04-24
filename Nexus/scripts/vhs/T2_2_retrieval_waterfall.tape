Output replaced.gif
Set FontSize 14
Set Width 1600
Set Height 1000
Set Theme "Dracula"
Set Shell "cmd"
Set TypingSpeed 80ms

Env NEXUS_API_URL=http://localhost:8080
Env NEXUS_ADMIN_TOKEN

Type "D:\bubblefish\nexus\nexus.exe tui"
Enter
Sleep 4s
Type "3"
Sleep 2s
Type "budget allocation Q3"
Enter
Sleep 4s
Screenshot retrieval_t1.png
Sleep 2s
Screenshot retrieval_t2.png
Type "q"
