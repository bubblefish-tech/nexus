2026-04-08T06:51:49.7583651Z Current runner version: '2.333.1'
2026-04-08T06:51:49.7619539Z ##[group]Runner Image Provisioner
2026-04-08T06:51:49.7620810Z Hosted Compute Agent
2026-04-08T06:51:49.7621697Z Version: 20260213.493
2026-04-08T06:51:49.7622653Z Commit: 5c115507f6dd24b8de37d8bbe0bb4509d0cc0fa3
2026-04-08T06:51:49.7623799Z Build Date: 2026-02-13T00:28:41Z
2026-04-08T06:51:49.7624909Z Worker ID: {639a6f4c-2da0-4510-a9cf-4d1d5a7846f0}
2026-04-08T06:51:49.7626046Z Azure Region: westus3
2026-04-08T06:51:49.7626890Z ##[endgroup]
2026-04-08T06:51:49.7629102Z ##[group]Operating System
2026-04-08T06:51:49.7630079Z Microsoft Windows Server 2025
2026-04-08T06:51:49.7631043Z 10.0.26100
2026-04-08T06:51:49.7631785Z Datacenter
2026-04-08T06:51:49.7632997Z ##[endgroup]
2026-04-08T06:51:49.7633842Z ##[group]Runner Image
2026-04-08T06:51:49.7634711Z Image: windows-2025
2026-04-08T06:51:49.7635646Z Version: 20260329.71.1
2026-04-08T06:51:49.7637888Z Included Software: https://github.com/actions/runner-images/blob/win25/20260329.71/images/windows/Windows2025-Readme.md
2026-04-08T06:51:49.7640616Z Image Release: https://github.com/actions/runner-images/releases/tag/win25%2F20260329.71
2026-04-08T06:51:49.7642260Z ##[endgroup]
2026-04-08T06:51:49.7644178Z ##[group]GITHUB_TOKEN Permissions
2026-04-08T06:51:49.7646692Z Contents: read
2026-04-08T06:51:49.7647441Z Metadata: read
2026-04-08T06:51:49.7648257Z Packages: read
2026-04-08T06:51:49.7649030Z ##[endgroup]
2026-04-08T06:51:49.7652228Z Secret source: Actions
2026-04-08T06:51:49.7653728Z Prepare workflow directory
2026-04-08T06:51:49.8515496Z Prepare all required actions
2026-04-08T06:51:49.8568938Z Getting action download info
2026-04-08T06:51:50.3445030Z Download action repository 'actions/checkout@v4' (SHA:34e114876b0b11c390a56381ad16ebd13914f8d5)
2026-04-08T06:51:50.4772058Z Download action repository 'actions/setup-go@v5' (SHA:40f1582b2485089dde7abd97c1529aa768e1baff)
2026-04-08T06:51:51.5543821Z Complete job name: test (windows-latest)
2026-04-08T06:51:51.6943238Z ##[group]Run actions/checkout@v4
2026-04-08T06:51:51.6944261Z with:
2026-04-08T06:51:51.6944729Z   repository: shawnsammartano-hub/BubbleFish-Nexus
2026-04-08T06:51:51.6945516Z   token: ***
2026-04-08T06:51:51.6945889Z   ssh-strict: true
2026-04-08T06:51:51.6946557Z   ssh-user: git
2026-04-08T06:51:51.6946970Z   persist-credentials: true
2026-04-08T06:51:51.6947411Z   clean: true
2026-04-08T06:51:51.6947821Z   sparse-checkout-cone-mode: true
2026-04-08T06:51:51.6948297Z   fetch-depth: 1
2026-04-08T06:51:51.6948680Z   fetch-tags: false
2026-04-08T06:51:51.6949069Z   show-progress: true
2026-04-08T06:51:51.6949486Z   lfs: false
2026-04-08T06:51:51.6949854Z   submodules: false
2026-04-08T06:51:51.6950257Z   set-safe-directory: true
2026-04-08T06:51:51.6950942Z ##[endgroup]
2026-04-08T06:51:51.9122005Z Syncing repository: shawnsammartano-hub/BubbleFish-Nexus
2026-04-08T06:51:51.9127932Z ##[group]Getting Git version info
2026-04-08T06:51:51.9130923Z Working directory is 'D:\a\BubbleFish-Nexus\BubbleFish-Nexus'
2026-04-08T06:51:52.5225377Z [command]"C:\Program Files\Git\bin\git.exe" version
2026-04-08T06:51:52.5227173Z git version 2.53.0.windows.2
2026-04-08T06:51:52.5233857Z ##[endgroup]
2026-04-08T06:51:52.5242031Z Temporarily overriding HOME='D:\a\_temp\c525293d-fcf8-41c4-b5c1-9d652eb5534c' before making global git config changes
2026-04-08T06:51:52.5245717Z Adding repository directory to the temporary git global config as a safe directory
2026-04-08T06:51:52.5250532Z [command]"C:\Program Files\Git\bin\git.exe" config --global --add safe.directory D:\a\BubbleFish-Nexus\BubbleFish-Nexus
2026-04-08T06:51:52.5257019Z Deleting the contents of 'D:\a\BubbleFish-Nexus\BubbleFish-Nexus'
2026-04-08T06:51:52.5259990Z ##[group]Initializing the repository
2026-04-08T06:51:52.5262542Z [command]"C:\Program Files\Git\bin\git.exe" init D:\a\BubbleFish-Nexus\BubbleFish-Nexus
2026-04-08T06:51:52.6093102Z Initialized empty Git repository in D:/a/BubbleFish-Nexus/BubbleFish-Nexus/.git/
2026-04-08T06:51:52.6155684Z [command]"C:\Program Files\Git\bin\git.exe" remote add origin https://github.com/shawnsammartano-hub/BubbleFish-Nexus
2026-04-08T06:51:52.6682905Z ##[endgroup]
2026-04-08T06:51:52.6685233Z ##[group]Disabling automatic garbage collection
2026-04-08T06:51:52.6695473Z [command]"C:\Program Files\Git\bin\git.exe" config --local gc.auto 0
2026-04-08T06:51:52.7008031Z ##[endgroup]
2026-04-08T06:51:52.7010378Z ##[group]Setting up auth
2026-04-08T06:51:52.7024137Z [command]"C:\Program Files\Git\bin\git.exe" config --local --name-only --get-regexp core\.sshCommand
2026-04-08T06:51:52.7349907Z [command]"C:\Program Files\Git\bin\git.exe" submodule foreach --recursive "sh -c \"git config --local --name-only --get-regexp 'core\.sshCommand' && git config --local --unset-all 'core.sshCommand' || :\""
2026-04-08T06:51:54.2082193Z [command]"C:\Program Files\Git\bin\git.exe" config --local --name-only --get-regexp http\.https\:\/\/github\.com\/\.extraheader
2026-04-08T06:51:54.2431195Z [command]"C:\Program Files\Git\bin\git.exe" submodule foreach --recursive "sh -c \"git config --local --name-only --get-regexp 'http\.https\:\/\/github\.com\/\.extraheader' && git config --local --unset-all 'http.https://github.com/.extraheader' || :\""
2026-04-08T06:51:54.8331007Z [command]"C:\Program Files\Git\bin\git.exe" config --local --name-only --get-regexp ^includeIf\.gitdir:
2026-04-08T06:51:54.8675142Z [command]"C:\Program Files\Git\bin\git.exe" submodule foreach --recursive "git config --local --show-origin --name-only --get-regexp remote.origin.url"
2026-04-08T06:51:55.4779185Z [command]"C:\Program Files\Git\bin\git.exe" config --local http.https://github.com/.extraheader "AUTHORIZATION: basic ***"
2026-04-08T06:51:55.5161958Z ##[endgroup]
2026-04-08T06:51:55.5166229Z ##[group]Fetching the repository
2026-04-08T06:51:55.5205908Z [command]"C:\Program Files\Git\bin\git.exe" -c protocol.version=2 fetch --no-tags --prune --no-recurse-submodules --depth=1 origin +30055c2f3a6fee416589ca5eb399396581ce52fa:refs/remotes/origin/main
2026-04-08T06:51:57.6168088Z From https://github.com/shawnsammartano-hub/BubbleFish-Nexus
2026-04-08T06:51:57.6172773Z  * [new ref]         30055c2f3a6fee416589ca5eb399396581ce52fa -> origin/main
2026-04-08T06:51:57.6590886Z ##[endgroup]
2026-04-08T06:51:57.6591827Z ##[group]Determining the checkout info
2026-04-08T06:51:57.6592843Z ##[endgroup]
2026-04-08T06:51:57.6604683Z [command]"C:\Program Files\Git\bin\git.exe" sparse-checkout disable
2026-04-08T06:51:57.7071866Z [command]"C:\Program Files\Git\bin\git.exe" config --local --unset-all extensions.worktreeConfig
2026-04-08T06:51:57.7370756Z ##[group]Checking out the ref
2026-04-08T06:51:57.7382850Z [command]"C:\Program Files\Git\bin\git.exe" checkout --progress --force -B main refs/remotes/origin/main
2026-04-08T06:51:58.5278754Z Switched to a new branch 'main'
2026-04-08T06:51:58.5301174Z branch 'main' set up to track 'origin/main'.
2026-04-08T06:51:58.5404071Z ##[endgroup]
2026-04-08T06:51:58.5841377Z [command]"C:\Program Files\Git\bin\git.exe" log -1 --format=%H
2026-04-08T06:51:58.6138695Z 30055c2f3a6fee416589ca5eb399396581ce52fa
2026-04-08T06:51:58.6580576Z ##[group]Run actions/setup-go@v5
2026-04-08T06:51:58.6580874Z with:
2026-04-08T06:51:58.6581053Z   go-version: 1.22
2026-04-08T06:51:58.6581237Z   check-latest: false
2026-04-08T06:51:58.6581554Z   token: ***
2026-04-08T06:51:58.6581706Z   cache: true
2026-04-08T06:51:58.6581878Z ##[endgroup]
2026-04-08T06:51:58.8952715Z Setup go version spec 1.22
2026-04-08T06:51:58.9144848Z Found in cache @ C:\hostedtoolcache\windows\go\1.22.12\x64
2026-04-08T06:51:58.9154083Z Added go to the path
2026-04-08T06:51:58.9159978Z Successfully set up Go version 1.22
2026-04-08T06:52:01.1381944Z [command]C:\hostedtoolcache\windows\go\1.22.12\x64\bin\go.exe env GOMODCACHE
2026-04-08T06:52:01.1382801Z [command]C:\hostedtoolcache\windows\go\1.22.12\x64\bin\go.exe env GOCACHE
2026-04-08T06:52:01.1383420Z C:\Users\runneradmin\go\pkg\mod
2026-04-08T06:52:01.1383823Z C:\Users\runneradmin\AppData\Local\go-build
2026-04-08T06:52:01.1411309Z ##[warning]Restore cache failed: Dependencies file is not found in D:\a\BubbleFish-Nexus\BubbleFish-Nexus. Supported file pattern: go.sum
2026-04-08T06:52:01.1441979Z go version go1.22.12 windows/amd64
2026-04-08T06:52:01.1442267Z 
2026-04-08T06:52:01.1442626Z ##[group]go env
2026-04-08T06:52:03.3588764Z set GO111MODULE=
2026-04-08T06:52:03.3589555Z set GOARCH=amd64
2026-04-08T06:52:03.3600050Z set GOBIN=
2026-04-08T06:52:03.3600654Z set GOCACHE=C:\Users\runneradmin\AppData\Local\go-build
2026-04-08T06:52:03.3603185Z set GOENV=C:\Users\runneradmin\AppData\Roaming\go\env
2026-04-08T06:52:03.3603783Z set GOEXE=.exe
2026-04-08T06:52:03.3604137Z set GOEXPERIMENT=
2026-04-08T06:52:03.3604480Z set GOFLAGS=
2026-04-08T06:52:03.3604816Z set GOHOSTARCH=amd64
2026-04-08T06:52:03.3605167Z set GOHOSTOS=windows
2026-04-08T06:52:03.3605517Z set GOINSECURE=
2026-04-08T06:52:03.3605911Z set GOMODCACHE=C:\Users\runneradmin\go\pkg\mod
2026-04-08T06:52:03.3614167Z set GONOPROXY=
2026-04-08T06:52:03.3614647Z set GONOSUMDB=
2026-04-08T06:52:03.3615087Z set GOOS=windows
2026-04-08T06:52:03.3616863Z set GOPATH=C:\Users\runneradmin\go
2026-04-08T06:52:03.3617725Z set GOPRIVATE=
2026-04-08T06:52:03.3618507Z set GOPROXY=https://proxy.golang.org,direct
2026-04-08T06:52:03.3619175Z set GOROOT=C:\hostedtoolcache\windows\go\1.22.12\x64
2026-04-08T06:52:03.3619754Z set GOSUMDB=sum.golang.org
2026-04-08T06:52:03.3620174Z set GOTMPDIR=
2026-04-08T06:52:03.3620524Z set GOTOOLCHAIN=auto
2026-04-08T06:52:03.3621143Z set GOTOOLDIR=C:\hostedtoolcache\windows\go\1.22.12\x64\pkg\tool\windows_amd64
2026-04-08T06:52:03.3621912Z set GOVCS=
2026-04-08T06:52:03.3642795Z set GOVERSION=go1.22.12
2026-04-08T06:52:03.3643509Z set GCCGO=gccgo
2026-04-08T06:52:03.3643949Z set GOAMD64=v1
2026-04-08T06:52:03.3644436Z set AR=ar
2026-04-08T06:52:03.3652227Z set CC=gcc
2026-04-08T06:52:03.3652733Z set CXX=g++
2026-04-08T06:52:03.3653182Z set CGO_ENABLED=1
2026-04-08T06:52:03.3654115Z set GOMOD=NUL
2026-04-08T06:52:03.3666433Z set GOWORK=
2026-04-08T06:52:03.3673234Z set CGO_CFLAGS=-O2 -g
2026-04-08T06:52:03.3673815Z set CGO_CPPFLAGS=
2026-04-08T06:52:03.3674267Z set CGO_CXXFLAGS=-O2 -g
2026-04-08T06:52:03.3674735Z set CGO_FFLAGS=-O2 -g
2026-04-08T06:52:03.3675210Z set CGO_LDFLAGS=-O2 -g
2026-04-08T06:52:03.3675709Z set PKG_CONFIG=pkg-config
2026-04-08T06:52:03.3677393Z set GOGCCFLAGS=-m64 -mthreads -Wl,--no-gc-sections -fmessage-length=0 -ffile-prefix-map=C:\Users\RUNNER~1\AppData\Local\Temp\go-build601047806=/tmp/go-build -gno-record-gcc-switches
2026-04-08T06:52:03.3678764Z 
2026-04-08T06:52:03.3681103Z ##[endgroup]
2026-04-08T06:52:03.3900936Z ##[group]Run go build ./...
2026-04-08T06:52:03.3901258Z [36;1mgo build ./...[0m
2026-04-08T06:52:03.4580664Z shell: C:\Program Files\PowerShell\7\pwsh.EXE -command ". '{0}'"
2026-04-08T06:52:03.4581138Z ##[endgroup]
2026-04-08T06:52:08.4237757Z go: downloading go1.26.1 (windows/amd64)
2026-04-08T06:52:29.2150247Z go: downloading github.com/jackc/pgx/v5 v5.9.1
2026-04-08T06:52:29.2184474Z go: downloading modernc.org/sqlite v1.48.1
2026-04-08T06:52:29.8904030Z go: downloading github.com/prometheus/client_golang v1.23.2
2026-04-08T06:52:30.1364983Z go: downloading github.com/BurntSushi/toml v1.6.0
2026-04-08T06:52:31.3782443Z go: downloading golang.org/x/sys v0.42.0
2026-04-08T06:52:32.0751728Z go: downloading github.com/go-chi/chi/v5 v5.2.5
2026-04-08T06:52:32.1868698Z go: downloading github.com/tidwall/gjson v1.18.0
2026-04-08T06:52:33.1025150Z go: downloading github.com/fsnotify/fsnotify v1.9.0
2026-04-08T06:52:33.2687806Z go: downloading github.com/golang-jwt/jwt/v5 v5.3.1
2026-04-08T06:52:34.0076391Z go: downloading github.com/beorn7/perks v1.0.1
2026-04-08T06:52:34.0735162Z go: downloading github.com/cespare/xxhash/v2 v2.3.0
2026-04-08T06:52:34.1295879Z go: downloading github.com/prometheus/client_model v0.6.2
2026-04-08T06:52:34.1883643Z go: downloading github.com/prometheus/common v0.66.1
2026-04-08T06:52:34.8308647Z go: downloading google.golang.org/protobuf v1.36.8
2026-04-08T06:52:36.6168423Z go: downloading github.com/tidwall/match v1.1.1
2026-04-08T06:52:36.6191972Z go: downloading github.com/tidwall/pretty v1.2.0
2026-04-08T06:52:36.6977726Z go: downloading github.com/jackc/pgpassfile v1.0.0
2026-04-08T06:52:36.7018805Z go: downloading github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761
2026-04-08T06:52:36.7393670Z go: downloading golang.org/x/text v0.29.0
2026-04-08T06:52:36.7494816Z go: downloading github.com/jackc/puddle/v2 v2.2.2
2026-04-08T06:52:36.8296470Z go: downloading github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822
2026-04-08T06:52:36.8823147Z go: downloading go.yaml.in/yaml/v2 v2.4.2
2026-04-08T06:52:36.9422306Z go: downloading modernc.org/libc v1.70.0
2026-04-08T06:52:42.9255212Z go: downloading golang.org/x/sync v0.19.0
2026-04-08T06:52:42.9275394Z go: downloading github.com/dustin/go-humanize v1.0.1
2026-04-08T06:52:42.9811505Z go: downloading github.com/mattn/go-isatty v0.0.20
2026-04-08T06:52:42.9886810Z go: downloading github.com/ncruces/go-strftime v1.0.0
2026-04-08T06:52:43.7013762Z go: downloading modernc.org/mathutil v1.7.1
2026-04-08T06:52:43.7036737Z go: downloading modernc.org/memory v1.11.0
2026-04-08T06:52:43.7891073Z go: downloading github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec
2026-04-08T06:53:44.2539896Z ##[group]Run go vet ./...
2026-04-08T06:53:44.2540196Z [36;1mgo vet ./...[0m
2026-04-08T06:53:44.2611384Z shell: C:\Program Files\PowerShell\7\pwsh.EXE -command ". '{0}'"
2026-04-08T06:53:44.2611722Z ##[endgroup]
2026-04-08T06:54:01.8946407Z ##[group]Run go test ./... -count=1
2026-04-08T06:54:01.8947004Z [36;1mgo test ./... -count=1[0m
2026-04-08T06:54:01.9019188Z shell: C:\Program Files\PowerShell\7\pwsh.EXE -command ". '{0}'"
2026-04-08T06:54:01.9019547Z ##[endgroup]
2026-04-08T06:54:26.1752926Z ok  	github.com/BubbleFish-Nexus/cmd/bubblefish	7.578s
2026-04-08T06:54:30.4738923Z ok  	github.com/BubbleFish-Nexus/internal/audit	11.376s
2026-04-08T06:54:30.4757501Z ok  	github.com/BubbleFish-Nexus/internal/backup	0.467s
2026-04-08T06:54:30.4760665Z --- FAIL: TestRunThroughput (0.01s)
2026-04-08T06:54:30.4762658Z     bench_test.go:277: p50 should be > 0
2026-04-08T06:54:30.4805402Z FAIL
2026-04-08T06:54:30.4807386Z FAIL	github.com/BubbleFish-Nexus/internal/bench	0.050s
2026-04-08T06:54:30.4809737Z ok  	github.com/BubbleFish-Nexus/internal/cache	0.032s
2026-04-08T06:54:30.4811667Z ok  	github.com/BubbleFish-Nexus/internal/config	0.245s
2026-04-08T06:54:59.9581294Z ok  	github.com/BubbleFish-Nexus/internal/daemon	28.826s
2026-04-08T06:54:59.9583370Z ok  	github.com/BubbleFish-Nexus/internal/demo	0.662s
2026-04-08T06:54:59.9584443Z ok  	github.com/BubbleFish-Nexus/internal/destination	5.479s
2026-04-08T06:54:59.9585491Z ok  	github.com/BubbleFish-Nexus/internal/doctor	0.058s
2026-04-08T06:54:59.9586441Z ok  	github.com/BubbleFish-Nexus/internal/embedding	5.099s
2026-04-08T06:54:59.9587761Z ok  	github.com/BubbleFish-Nexus/internal/eventsink	1.455s
2026-04-08T06:54:59.9589612Z ok  	github.com/BubbleFish-Nexus/internal/firewall	0.134s
2026-04-08T06:54:59.9590570Z ok  	github.com/BubbleFish-Nexus/internal/fsutil	0.050s
2026-04-08T06:54:59.9591514Z ok  	github.com/BubbleFish-Nexus/internal/hotreload	0.112s
2026-04-08T06:54:59.9593025Z ok  	github.com/BubbleFish-Nexus/internal/idempotency	0.048s
2026-04-08T06:54:59.9594565Z ok  	github.com/BubbleFish-Nexus/internal/jwtauth	0.531s
2026-04-08T06:54:59.9596154Z ok  	github.com/BubbleFish-Nexus/internal/lint	0.112s
2026-04-08T06:54:59.9597042Z ok  	github.com/BubbleFish-Nexus/internal/mcp	0.108s
2026-04-08T06:54:59.9597811Z ok  	github.com/BubbleFish-Nexus/internal/metrics	0.048s
2026-04-08T06:54:59.9598525Z ok  	github.com/BubbleFish-Nexus/internal/oauth	3.064s
2026-04-08T06:54:59.9599355Z ok  	github.com/BubbleFish-Nexus/internal/policy	0.091s
2026-04-08T06:54:59.9600199Z ok  	github.com/BubbleFish-Nexus/internal/projection	0.028s
2026-04-08T06:54:59.9887432Z ok  	github.com/BubbleFish-Nexus/internal/query	0.508s
2026-04-08T06:55:00.9184117Z ok  	github.com/BubbleFish-Nexus/internal/queue	0.093s
2026-04-08T06:55:00.9717635Z ok  	github.com/BubbleFish-Nexus/internal/securitylog	0.050s
2026-04-08T06:55:01.3373032Z ok  	github.com/BubbleFish-Nexus/internal/signing	0.278s
2026-04-08T06:55:01.5120736Z ok  	github.com/BubbleFish-Nexus/internal/tray	0.031s
2026-04-08T06:55:01.5154120Z ?   	github.com/BubbleFish-Nexus/internal/version	[no test files]
2026-04-08T06:55:02.0704558Z ok  	github.com/BubbleFish-Nexus/internal/vizpipe	0.181s
2026-04-08T06:55:03.9799482Z ok  	github.com/BubbleFish-Nexus/internal/wal	1.794s
2026-04-08T06:55:03.9800523Z ok  	github.com/BubbleFish-Nexus/internal/web	0.029s
2026-04-08T06:55:03.9801314Z FAIL
2026-04-08T06:55:04.4285359Z ##[error]Process completed with exit code 1.
2026-04-08T06:55:04.4515080Z Post job cleanup.
2026-04-08T06:55:04.7882798Z [command]"C:\Program Files\Git\bin\git.exe" version
2026-04-08T06:55:04.8334767Z git version 2.53.0.windows.2
2026-04-08T06:55:04.8441846Z Temporarily overriding HOME='D:\a\_temp\914405b7-c791-427f-ad21-a3928a75b9e0' before making global git config changes
2026-04-08T06:55:04.8443533Z Adding repository directory to the temporary git global config as a safe directory
2026-04-08T06:55:04.8456353Z [command]"C:\Program Files\Git\bin\git.exe" config --global --add safe.directory D:\a\BubbleFish-Nexus\BubbleFish-Nexus
2026-04-08T06:55:04.8783567Z [command]"C:\Program Files\Git\bin\git.exe" config --local --name-only --get-regexp core\.sshCommand
2026-04-08T06:55:04.9140825Z [command]"C:\Program Files\Git\bin\git.exe" submodule foreach --recursive "sh -c \"git config --local --name-only --get-regexp 'core\.sshCommand' && git config --local --unset-all 'core.sshCommand' || :\""
2026-04-08T06:55:05.5999500Z [command]"C:\Program Files\Git\bin\git.exe" config --local --name-only --get-regexp http\.https\:\/\/github\.com\/\.extraheader
2026-04-08T06:55:05.6268507Z http.https://github.com/.extraheader
2026-04-08T06:55:05.6356262Z [command]"C:\Program Files\Git\bin\git.exe" config --local --unset-all http.https://github.com/.extraheader
2026-04-08T06:55:05.6660911Z [command]"C:\Program Files\Git\bin\git.exe" submodule foreach --recursive "sh -c \"git config --local --name-only --get-regexp 'http\.https\:\/\/github\.com\/\.extraheader' && git config --local --unset-all 'http.https://github.com/.extraheader' || :\""
2026-04-08T06:55:06.2520730Z [command]"C:\Program Files\Git\bin\git.exe" config --local --name-only --get-regexp ^includeIf\.gitdir:
2026-04-08T06:55:06.2846687Z [command]"C:\Program Files\Git\bin\git.exe" submodule foreach --recursive "git config --local --show-origin --name-only --get-regexp remote.origin.url"
2026-04-08T06:55:06.8665648Z Cleaning up orphan processes
2026-04-08T06:55:06.8706003Z ##[warning]Node.js 20 actions are deprecated. The following actions are running on Node.js 20 and may not work as expected: actions/checkout@v4, actions/setup-go@v5. Actions will be forced to run with Node.js 24 by default starting June 2nd, 2026. Node.js 20 will be removed from the runner on September 16th, 2026. Please check if updated versions of these actions are available that support Node.js 24. To opt into Node.js 24 now, set the FORCE_JAVASCRIPT_ACTIONS_TO_NODE24=true environment variable on the runner or in your workflow file. Once Node.js 24 becomes the default, you can temporarily opt out by setting ACTIONS_ALLOW_USE_UNSECURE_NODE_VERSION=true. For more information see: https://github.blog/changelog/2025-09-19-deprecation-of-node-20-on-github-actions-runners/
