Output scripts/vhs/out/T2_checkpoint.gif
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
Sleep 4s
Screenshot scripts/vhs/out/T2_1_dashboard.png
Type "2"
Sleep 3s
Type "/"
Type "test"
Enter
Sleep 2s
Screenshot scripts/vhs/out/T2_2_memory.png
Type "3"
Sleep 2s
Screenshot scripts/vhs/out/T2_3_retrieval.png
Type "4"
Sleep 3s
Screenshot scripts/vhs/out/T2_4_audit.png
Type "5"
Sleep 3s
Screenshot scripts/vhs/out/T2_5_agents.png
Type "6"
Sleep 3s
Screenshot scripts/vhs/out/T2_6_crypto.png
Type "7"
Sleep 3s
Screenshot scripts/vhs/out/T2_7_governance.png
Type "8"
Sleep 3s
Screenshot scripts/vhs/out/T2_8_immune.png
Type "q"
