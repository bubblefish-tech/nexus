Output scripts/vhs/out/T2_2_retrieval_waterfall.gif
Set FontSize 14
Set Width 1600
Set Height 1000
Set Theme "Dracula"
Set Shell "cmd"
Set TypingSpeed 80ms

Env NEXUS_API_URL "http://localhost:8080"
Env NEXUS_ADMIN_TOKEN "bfn_admin_REDACTED4f154216b48bc89be09821de1fbe488d1df16b12f2ea339a9c8de750b1"

Type "D:\bubblefish\nexus\nexus.exe tui"
Enter
Sleep 4s
Type "3"
Sleep 2s
Type "budget allocation Q3"
Enter
Sleep 4s
Screenshot scripts/vhs/out/T2_2_retrieval_t1.png
Sleep 2s
Screenshot scripts/vhs/out/T2_2_retrieval_t2.png
Type "q"
