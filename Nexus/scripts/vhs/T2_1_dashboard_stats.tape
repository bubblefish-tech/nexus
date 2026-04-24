Output scripts/vhs/out/T2_1_dashboard_stats.gif
Set FontSize 14
Set Width 1600
Set Height 1000
Set Theme "Dracula"
Set Shell "cmd"
Set TypingSpeed 50ms

Env NEXUS_API_URL "http://localhost:8080"
Env NEXUS_ADMIN_TOKEN "bfn_admin_REDACTED4f154216b48bc89be09821de1fbe488d1df16b12f2ea339a9c8de750b1"

Type "D:\bubblefish\nexus\nexus.exe tui"
Enter
Sleep 5s
Screenshot scripts/vhs/out/T2_1_dashboard_t1.png
Sleep 3s
Screenshot scripts/vhs/out/T2_1_dashboard_t2.png
Type "q"
