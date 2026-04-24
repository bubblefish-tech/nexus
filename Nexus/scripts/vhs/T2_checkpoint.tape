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
Screenshot 1_dashboard.png
Type "2"
Sleep 3s
Type "/"
Type "test"
Enter
Sleep 2s
Screenshot 2_memory.png
Type "3"
Sleep 2s
Screenshot 3_retrieval.png
Type "4"
Sleep 3s
Screenshot 4_audit.png
Type "5"
Sleep 3s
Screenshot 5_agents.png
Type "6"
Sleep 3s
Screenshot 6_crypto.png
Type "7"
Sleep 3s
Screenshot 7_governance.png
Type "8"
Sleep 3s
Screenshot 8_immune.png
Type "q"
