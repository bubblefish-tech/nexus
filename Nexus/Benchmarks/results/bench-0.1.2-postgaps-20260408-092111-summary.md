# Benchmark Suite Results — bench-0.1.2-postgaps-20260408-092111

- **Version:** 0.1.2
- **Git SHA:** 8c8d57c68061b8c9a3d88658b8dfb6c988af371f
- **Timestamp:** 2026-04-08T09:27:16-07:00
- **OS/Arch:** windows/amd64
- **CPU:** AMD Ryzen 7 7735HS with Radeon Graphics
- **Go version:** go version go1.26.1 windows/amd64
- **Total benchmarks:** 20 (100 result lines @ count=5)
- **Packages benchmarked:** 8 (wal, queue, mcp, embedding, cache, jwtauth, projection, firewall)
- **Failures/panics:** 0

## All Benchmark Results (first run)
```
BenchmarkCache_Hit-16     	32445410	        37.13 ns/op	       0 B/op	       0 allocs/op
BenchmarkCache_Miss-16    	40740802	        28.85 ns/op	       0 B/op	       0 allocs/op
BenchmarkCache_Set-16     	  828099	      1278 ns/op	     529 B/op	      11 allocs/op
BenchmarkEmbedding_Generate_ShortText-16        	    7914	    139119 ns/op	   18664 B/op	     128 allocs/op
BenchmarkEmbedding_Generate_ParagraphText-16    	    5726	    214203 ns/op	   19382 B/op	     129 allocs/op
BenchmarkPostFilter_1000Records-16    	   16279	     70361 ns/op	  335874 B/op	       1 allocs/op
BenchmarkJWT_Validate_ValidToken-16      	   40393	     29451 ns/op	    3424 B/op	      52 allocs/op
BenchmarkJWT_Validate_ExpiredToken-16    	   29438	     42426 ns/op	    3416 B/op	      52 allocs/op
BenchmarkMCP_NexusStatus-16               	2026/04/08 09:22:47 INFO mcp: server started component=mcp addr=127.0.0.1:59657
BenchmarkMCP_NexusWrite_SmallMemory-16    	2026/04/08 09:22:53 INFO mcp: server started component=mcp addr=127.0.0.1:59687
BenchmarkMCP_NexusSearch_10Results-16     	2026/04/08 09:22:59 INFO mcp: server started component=mcp addr=127.0.0.1:59718
BenchmarkProjection_Stage_End2End-16       	    1510	    795505 ns/op	  401785 B/op	    7003 allocs/op
BenchmarkQueue_Enqueue_Single-16       	 8618812	       127.7 ns/op	      63 B/op	       2 allocs/op
BenchmarkQueue_Dequeue_Single-16       	 1000000	      1058 ns/op	     495 B/op	       5 allocs/op
BenchmarkQueue_DrainToSQLite_100-16    	    5090	    245228 ns/op	  113526 B/op	    1139 allocs/op
BenchmarkWAL_Append_SmallEntry-16     	    1269	   1016361 ns/op	    1333 B/op	      10 allocs/op
BenchmarkWAL_Append_LargeEntry-16     	    1252	    957208 ns/op	   10107 B/op	      10 allocs/op
BenchmarkWAL_Append_Batch100-16       	      12	  94418067 ns/op	  131470 B/op	     903 allocs/op
BenchmarkWAL_Replay_1000Entries-16    	     122	   9438624 ns/op	12401292 B/op	   18068 allocs/op
BenchmarkWAL_MarkStatus-16            	      73	  16223003 ns/op	10681087 B/op	    1303 allocs/op
```

## Files
- Raw: D:\Bubblefish\Nexus\Benchmarks\results\bench-0.1.2-postgaps-20260408-092111-raw.txt
- Cleaned: D:\Bubblefish\Nexus\Benchmarks\results\bench-0.1.2-postgaps-20260408-092111.txt
- Summary: D:\Bubblefish\Nexus\Benchmarks\results\bench-0.1.2-postgaps-20260408-092111-summary.md
