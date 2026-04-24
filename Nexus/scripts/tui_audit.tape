Output scripts/tui_audit.gif
Set FontSize 14
Set Width 1600
Set Height 1000
Set Theme "Dracula"
Set Shell "cmd"

Env NEXUS_API_URL "http://localhost:8080"
Env NEXUS_ADMIN_TOKEN "bfn_admin_REDACTED4f154216b48bc89be09821de1fbe488d1df16b12f2ea339a9c8de750b1"

Type "D:\bubblefish\nexus\nexus.exe tui"
Enter
Sleep 5s
Type "1"
Sleep 3s
Screenshot scripts/dashboard.png
Type "2"
Sleep 2s
Screenshot scripts/memory.png
Type "3"
Sleep 2s
Screenshot scripts/retrieval.png
Type "4"
Sleep 2s
Screenshot scripts/audit.png
Type "5"
Sleep 2s
Screenshot scripts/agents.png
Type "6"
Sleep 2s
Screenshot scripts/crypto.png
Type "7"
Sleep 2s
Screenshot scripts/governance.png
Type "8"
Sleep 2s
Screenshot scripts/immune.png
Type "q"
