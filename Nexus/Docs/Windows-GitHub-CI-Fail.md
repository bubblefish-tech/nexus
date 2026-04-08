2026-04-08T06:27:21.1400358Z Current runner version: '2.333.1'
2026-04-08T06:27:21.1658320Z ##[group]Runner Image Provisioner
2026-04-08T06:27:21.1659493Z Hosted Compute Agent
2026-04-08T06:27:21.1660431Z Version: 20260213.493
2026-04-08T06:27:21.1661369Z Commit: 5c115507f6dd24b8de37d8bbe0bb4509d0cc0fa3
2026-04-08T06:27:21.1662562Z Build Date: 2026-02-13T00:28:41Z
2026-04-08T06:27:21.1663682Z Worker ID: {fa0aaf49-6409-4d06-af24-8f8ea615ae9c}
2026-04-08T06:27:21.1664851Z Azure Region: centralus
2026-04-08T06:27:21.1665766Z ##[endgroup]
2026-04-08T06:27:21.1667937Z ##[group]Operating System
2026-04-08T06:27:21.1669261Z Microsoft Windows Server 2025
2026-04-08T06:27:21.1670150Z 10.0.26100
2026-04-08T06:27:21.1670855Z Datacenter
2026-04-08T06:27:21.1671595Z ##[endgroup]
2026-04-08T06:27:21.1672388Z ##[group]Runner Image
2026-04-08T06:27:21.1673240Z Image: windows-2025
2026-04-08T06:27:21.1674047Z Version: 20260329.71.1
2026-04-08T06:27:21.1676383Z Included Software: https://github.com/actions/runner-images/blob/win25/20260329.71/images/windows/Windows2025-Readme.md
2026-04-08T06:27:21.1678933Z Image Release: https://github.com/actions/runner-images/releases/tag/win25%2F20260329.71
2026-04-08T06:27:21.1680388Z ##[endgroup]
2026-04-08T06:27:21.1682198Z ##[group]GITHUB_TOKEN Permissions
2026-04-08T06:27:21.1684587Z Contents: read
2026-04-08T06:27:21.1685416Z Metadata: read
2026-04-08T06:27:21.1686157Z Packages: read
2026-04-08T06:27:21.1686970Z ##[endgroup]
2026-04-08T06:27:21.1690278Z Secret source: Actions
2026-04-08T06:27:21.1691352Z Prepare workflow directory
2026-04-08T06:27:21.3140155Z Prepare all required actions
2026-04-08T06:27:21.3179919Z Getting action download info
2026-04-08T06:27:21.6163527Z Download action repository 'actions/checkout@v4' (SHA:34e114876b0b11c390a56381ad16ebd13914f8d5)
2026-04-08T06:27:21.8239485Z Download action repository 'actions/setup-go@v5' (SHA:40f1582b2485089dde7abd97c1529aa768e1baff)
2026-04-08T06:27:22.9256933Z Complete job name: test (windows-latest)
2026-04-08T06:27:23.7626162Z ##[group]Run actions/checkout@v4
2026-04-08T06:27:23.7627257Z with:
2026-04-08T06:27:23.7627749Z   repository: shawnsammartano-hub/BubbleFish-Nexus
2026-04-08T06:27:23.7628664Z   token: ***
2026-04-08T06:27:23.7629043Z   ssh-strict: true
2026-04-08T06:27:23.7629446Z   ssh-user: git
2026-04-08T06:27:23.7629859Z   persist-credentials: true
2026-04-08T06:27:23.7630301Z   clean: true
2026-04-08T06:27:23.7630700Z   sparse-checkout-cone-mode: true
2026-04-08T06:27:23.7631183Z   fetch-depth: 1
2026-04-08T06:27:23.7631571Z   fetch-tags: false
2026-04-08T06:27:23.7631976Z   show-progress: true
2026-04-08T06:27:23.7632382Z   lfs: false
2026-04-08T06:27:23.7632760Z   submodules: false
2026-04-08T06:27:23.7633191Z   set-safe-directory: true
2026-04-08T06:27:23.7633858Z ##[endgroup]
2026-04-08T06:27:24.6911093Z Syncing repository: shawnsammartano-hub/BubbleFish-Nexus
2026-04-08T06:27:24.6914079Z ##[group]Getting Git version info
2026-04-08T06:27:24.6915782Z Working directory is 'D:\a\BubbleFish-Nexus\BubbleFish-Nexus'
2026-04-08T06:27:24.6918439Z [command]"C:\Program Files\Git\bin\git.exe" version
2026-04-08T06:27:24.6919915Z git version 2.53.0.windows.2
2026-04-08T06:27:24.6924340Z ##[endgroup]
2026-04-08T06:27:25.7122551Z Temporarily overriding HOME='D:\a\_temp\c436ec18-5b42-4d59-ab23-cffab684330b' before making global git config changes
2026-04-08T06:27:25.7126866Z Adding repository directory to the temporary git global config as a safe directory
2026-04-08T06:27:25.7128338Z [command]"C:\Program Files\Git\bin\git.exe" config --global --add safe.directory D:\a\BubbleFish-Nexus\BubbleFish-Nexus
2026-04-08T06:27:25.7130878Z Deleting the contents of 'D:\a\BubbleFish-Nexus\BubbleFish-Nexus'
2026-04-08T06:27:25.7132076Z ##[group]Initializing the repository
2026-04-08T06:27:25.7133104Z [command]"C:\Program Files\Git\bin\git.exe" init D:\a\BubbleFish-Nexus\BubbleFish-Nexus
2026-04-08T06:27:25.7134246Z Initialized empty Git repository in D:/a/BubbleFish-Nexus/BubbleFish-Nexus/.git/
2026-04-08T06:27:25.7445624Z [command]"C:\Program Files\Git\bin\git.exe" remote add origin https://github.com/shawnsammartano-hub/BubbleFish-Nexus
2026-04-08T06:27:25.7447648Z ##[endgroup]
2026-04-08T06:27:25.7448501Z ##[group]Disabling automatic garbage collection
2026-04-08T06:27:25.7449256Z [command]"C:\Program Files\Git\bin\git.exe" config --local gc.auto 0
2026-04-08T06:27:25.7466000Z ##[endgroup]
2026-04-08T06:27:25.7466775Z ##[group]Setting up auth
2026-04-08T06:27:25.7467626Z [command]"C:\Program Files\Git\bin\git.exe" config --local --name-only --get-regexp core\.sshCommand
2026-04-08T06:27:25.7469950Z [command]"C:\Program Files\Git\bin\git.exe" submodule foreach --recursive "sh -c \"git config --local --name-only --get-regexp 'core\.sshCommand' && git config --local --unset-all 'core.sshCommand' || :\""
2026-04-08T06:27:26.6768751Z [command]"C:\Program Files\Git\bin\git.exe" config --local --name-only --get-regexp http\.https\:\/\/github\.com\/\.extraheader
2026-04-08T06:27:26.6772304Z [command]"C:\Program Files\Git\bin\git.exe" submodule foreach --recursive "sh -c \"git config --local --name-only --get-regexp 'http\.https\:\/\/github\.com\/\.extraheader' && git config --local --unset-all 'http.https://github.com/.extraheader' || :\""
2026-04-08T06:27:27.1545496Z [command]"C:\Program Files\Git\bin\git.exe" config --local --name-only --get-regexp ^includeIf\.gitdir:
2026-04-08T06:27:27.1898045Z [command]"C:\Program Files\Git\bin\git.exe" submodule foreach --recursive "git config --local --show-origin --name-only --get-regexp remote.origin.url"
2026-04-08T06:27:27.7650125Z [command]"C:\Program Files\Git\bin\git.exe" config --local http.https://github.com/.extraheader "AUTHORIZATION: basic ***"
2026-04-08T06:27:27.7993929Z ##[endgroup]
2026-04-08T06:27:27.7995023Z ##[group]Fetching the repository
2026-04-08T06:27:27.8122808Z [command]"C:\Program Files\Git\bin\git.exe" -c protocol.version=2 fetch --no-tags --prune --no-recurse-submodules --depth=1 origin +6998d7efc4855e2382c8450d39daae9f60df3d62:refs/remotes/origin/main
2026-04-08T06:27:31.2788159Z From https://github.com/shawnsammartano-hub/BubbleFish-Nexus
2026-04-08T06:27:31.2789586Z  * [new ref]         6998d7efc4855e2382c8450d39daae9f60df3d62 -> origin/main
2026-04-08T06:27:31.3309037Z ##[endgroup]
2026-04-08T06:27:31.3340140Z ##[group]Determining the checkout info
2026-04-08T06:27:31.3348714Z ##[endgroup]
2026-04-08T06:27:31.3349501Z [command]"C:\Program Files\Git\bin\git.exe" sparse-checkout disable
2026-04-08T06:27:31.3883140Z [command]"C:\Program Files\Git\bin\git.exe" config --local --unset-all extensions.worktreeConfig
2026-04-08T06:27:31.4214536Z ##[group]Checking out the ref
2026-04-08T06:27:31.4215583Z [command]"C:\Program Files\Git\bin\git.exe" checkout --progress --force -B main refs/remotes/origin/main
2026-04-08T06:27:32.2284104Z Switched to a new branch 'main'
2026-04-08T06:27:32.2318064Z branch 'main' set up to track 'origin/main'.
2026-04-08T06:27:32.2433406Z ##[endgroup]
2026-04-08T06:27:32.2907325Z [command]"C:\Program Files\Git\bin\git.exe" log -1 --format=%H
2026-04-08T06:27:32.3238555Z 6998d7efc4855e2382c8450d39daae9f60df3d62
2026-04-08T06:27:32.3686899Z ##[group]Run actions/setup-go@v5
2026-04-08T06:27:32.3687282Z with:
2026-04-08T06:27:32.3687454Z   go-version: 1.22
2026-04-08T06:27:32.3687621Z   check-latest: false
2026-04-08T06:27:32.3687951Z   token: ***
2026-04-08T06:27:32.3688105Z   cache: true
2026-04-08T06:27:32.3688264Z ##[endgroup]
2026-04-08T06:27:32.6322554Z Setup go version spec 1.22
2026-04-08T06:27:32.6569599Z Found in cache @ C:\hostedtoolcache\windows\go\1.22.12\x64
2026-04-08T06:27:32.6571148Z Added go to the path
2026-04-08T06:27:32.6573038Z Successfully set up Go version 1.22
2026-04-08T06:27:34.0576747Z [command]C:\hostedtoolcache\windows\go\1.22.12\x64\bin\go.exe env GOMODCACHE
2026-04-08T06:27:34.0691524Z [command]C:\hostedtoolcache\windows\go\1.22.12\x64\bin\go.exe env GOCACHE
2026-04-08T06:27:34.1118510Z C:\Users\runneradmin\go\pkg\mod
2026-04-08T06:27:34.1122667Z C:\Users\runneradmin\AppData\Local\go-build
2026-04-08T06:27:34.1216385Z ##[warning]Restore cache failed: Dependencies file is not found in D:\a\BubbleFish-Nexus\BubbleFish-Nexus. Supported file pattern: go.sum
2026-04-08T06:27:34.1246724Z go version go1.22.12 windows/amd64
2026-04-08T06:27:34.1247258Z 
2026-04-08T06:27:34.1247850Z ##[group]go env
2026-04-08T06:27:37.6015219Z set GO111MODULE=
2026-04-08T06:27:37.6015996Z set GOARCH=amd64
2026-04-08T06:27:37.6016473Z set GOBIN=
2026-04-08T06:27:37.6027222Z set GOCACHE=C:\Users\runneradmin\AppData\Local\go-build
2026-04-08T06:27:37.6029932Z set GOENV=C:\Users\runneradmin\AppData\Roaming\go\env
2026-04-08T06:27:37.6030968Z set GOEXE=.exe
2026-04-08T06:27:37.6031399Z set GOEXPERIMENT=
2026-04-08T06:27:37.6031805Z set GOFLAGS=
2026-04-08T06:27:37.6032200Z set GOHOSTARCH=amd64
2026-04-08T06:27:37.6032632Z set GOHOSTOS=windows
2026-04-08T06:27:37.6033034Z set GOINSECURE=
2026-04-08T06:27:37.6033508Z set GOMODCACHE=C:\Users\runneradmin\go\pkg\mod
2026-04-08T06:27:37.6034089Z set GONOPROXY=
2026-04-08T06:27:37.6034483Z set GONOSUMDB=
2026-04-08T06:27:37.6034874Z set GOOS=windows
2026-04-08T06:27:37.6035306Z set GOPATH=C:\Users\runneradmin\go
2026-04-08T06:27:37.6035800Z set GOPRIVATE=
2026-04-08T06:27:37.6036280Z set GOPROXY=https://proxy.golang.org,direct
2026-04-08T06:27:37.6036943Z set GOROOT=C:\hostedtoolcache\windows\go\1.22.12\x64
2026-04-08T06:27:37.6037543Z set GOSUMDB=sum.golang.org
2026-04-08T06:27:37.6038005Z set GOTMPDIR=
2026-04-08T06:27:37.6038480Z set GOTOOLCHAIN=auto
2026-04-08T06:27:37.6039241Z set GOTOOLDIR=C:\hostedtoolcache\windows\go\1.22.12\x64\pkg\tool\windows_amd64
2026-04-08T06:27:37.6060623Z set GOVCS=
2026-04-08T06:27:37.6061155Z set GOVERSION=go1.22.12
2026-04-08T06:27:37.6061653Z set GCCGO=gccgo
2026-04-08T06:27:37.6062073Z set GOAMD64=v1
2026-04-08T06:27:37.6062563Z set AR=ar
2026-04-08T06:27:37.6065743Z set CC=gcc
2026-04-08T06:27:37.6069124Z set CXX=g++
2026-04-08T06:27:37.6070093Z set CGO_ENABLED=1
2026-04-08T06:27:37.6070624Z set GOMOD=NUL
2026-04-08T06:27:37.6071221Z set GOWORK=
2026-04-08T06:27:37.6071743Z set CGO_CFLAGS=-O2 -g
2026-04-08T06:27:37.6072227Z set CGO_CPPFLAGS=
2026-04-08T06:27:37.6072615Z set CGO_CXXFLAGS=-O2 -g
2026-04-08T06:27:37.6073021Z set CGO_FFLAGS=-O2 -g
2026-04-08T06:27:37.6073438Z set CGO_LDFLAGS=-O2 -g
2026-04-08T06:27:37.6073916Z set PKG_CONFIG=pkg-config
2026-04-08T06:27:37.6075261Z set GOGCCFLAGS=-m64 -mthreads -Wl,--no-gc-sections -fmessage-length=0 -ffile-prefix-map=C:\Users\RUNNER~1\AppData\Local\Temp\go-build2441980508=/tmp/go-build -gno-record-gcc-switches
2026-04-08T06:27:37.6076487Z 
2026-04-08T06:27:37.6077102Z ##[endgroup]
2026-04-08T06:27:37.6307813Z ##[group]Run go build ./...
2026-04-08T06:27:37.6308730Z [36;1mgo build ./...[0m
2026-04-08T06:27:37.7017388Z shell: C:\Program Files\PowerShell\7\pwsh.EXE -command ". '{0}'"
2026-04-08T06:27:37.7017829Z ##[endgroup]
2026-04-08T06:27:43.4227111Z go: downloading go1.26.1 (windows/amd64)
2026-04-08T06:28:12.7134470Z go: downloading github.com/jackc/pgx/v5 v5.9.1
2026-04-08T06:28:12.9968292Z go: downloading modernc.org/sqlite v1.48.1
2026-04-08T06:28:13.6743918Z go: downloading github.com/prometheus/client_golang v1.23.2
2026-04-08T06:28:13.9823169Z go: downloading github.com/BurntSushi/toml v1.6.0
2026-04-08T06:28:16.3034693Z go: downloading golang.org/x/sys v0.42.0
2026-04-08T06:28:18.6466702Z go: downloading github.com/go-chi/chi/v5 v5.2.5
2026-04-08T06:28:18.9193463Z go: downloading github.com/tidwall/gjson v1.18.0
2026-04-08T06:28:19.0957689Z go: downloading github.com/fsnotify/fsnotify v1.9.0
2026-04-08T06:28:19.4949795Z go: downloading github.com/golang-jwt/jwt/v5 v5.3.1
2026-04-08T06:28:21.6976633Z go: downloading github.com/beorn7/perks v1.0.1
2026-04-08T06:28:21.6982302Z go: downloading github.com/cespare/xxhash/v2 v2.3.0
2026-04-08T06:28:21.7832051Z go: downloading github.com/prometheus/client_model v0.6.2
2026-04-08T06:28:21.7839642Z go: downloading github.com/prometheus/common v0.66.1
2026-04-08T06:28:22.0119242Z go: downloading google.golang.org/protobuf v1.36.8
2026-04-08T06:28:23.3348897Z go: downloading github.com/tidwall/match v1.1.1
2026-04-08T06:28:23.3349548Z go: downloading github.com/tidwall/pretty v1.2.0
2026-04-08T06:28:23.4157702Z go: downloading modernc.org/libc v1.70.0
2026-04-08T06:28:29.8646038Z go: downloading github.com/jackc/pgpassfile v1.0.0
2026-04-08T06:28:29.8674368Z go: downloading github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761
2026-04-08T06:28:29.9356784Z go: downloading golang.org/x/text v0.29.0
2026-04-08T06:28:29.9511498Z go: downloading github.com/jackc/puddle/v2 v2.2.2
2026-04-08T06:28:30.0541918Z go: downloading github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822
2026-04-08T06:28:30.1275215Z go: downloading go.yaml.in/yaml/v2 v2.4.2
2026-04-08T06:28:30.2781557Z go: downloading github.com/dustin/go-humanize v1.0.1
2026-04-08T06:28:30.3648649Z go: downloading github.com/mattn/go-isatty v0.0.20
2026-04-08T06:28:30.4408404Z go: downloading github.com/ncruces/go-strftime v1.0.0
2026-04-08T06:28:30.9254312Z go: downloading modernc.org/mathutil v1.7.1
2026-04-08T06:28:31.0667075Z go: downloading modernc.org/memory v1.11.0
2026-04-08T06:28:31.2198167Z go: downloading golang.org/x/sync v0.19.0
2026-04-08T06:28:31.3242287Z go: downloading github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec
2026-04-08T06:29:35.5342802Z ##[group]Run go vet ./...
2026-04-08T06:29:35.5343102Z [36;1mgo vet ./...[0m
2026-04-08T06:29:35.5413560Z shell: C:\Program Files\PowerShell\7\pwsh.EXE -command ". '{0}'"
2026-04-08T06:29:35.5413888Z ##[endgroup]
2026-04-08T06:29:53.9157874Z ##[group]Run go test ./... -count=1
2026-04-08T06:29:53.9158587Z [36;1mgo test ./... -count=1[0m
2026-04-08T06:29:53.9267984Z shell: C:\Program Files\PowerShell\7\pwsh.EXE -command ". '{0}'"
2026-04-08T06:29:53.9268762Z ##[endgroup]
2026-04-08T06:30:18.7805015Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestWriteConfigFile4085593362\001\test.toml
2026-04-08T06:30:18.7806570Z   skip (exists): C:\Users\RUNNER~1\AppData\Local\Temp\TestWriteConfigFile4085593362\001\test.toml
2026-04-08T06:30:18.7807269Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestWriteConfigFile4085593362\001\test.toml
2026-04-08T06:30:18.7807977Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestWriteDestination2038034889\001\destinations\sqlite.toml
2026-04-08T06:30:18.7808650Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallSQLite1173780198\001\daemon.toml
2026-04-08T06:30:18.7809472Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallSQLite1173780198\001\destinations\sqlite.toml
2026-04-08T06:30:18.7810175Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallSQLite1173780198\001\sources\default.toml
2026-04-08T06:30:18.7812261Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallPostgresPrompted3016648319\001\daemon.toml
2026-04-08T06:30:18.7813084Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallPostgresPrompted3016648319\001\destinations\postgres.toml
2026-04-08T06:30:18.7814510Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallPostgresPrompted3016648319\001\sources\default.toml
2026-04-08T06:30:18.7815637Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallPostgresEnvRef3047432758\001\daemon.toml
2026-04-08T06:30:18.7817454Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallPostgresEnvRef3047432758\001\destinations\postgres.toml
2026-04-08T06:30:18.7818709Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallPostgresEnvRef3047432758\001\sources\default.toml
2026-04-08T06:30:18.7820044Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallOpenBrainPrompted1255845941\001\daemon.toml
2026-04-08T06:30:18.7823423Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallOpenBrainPrompted1255845941\001\destinations\openbrain.toml
2026-04-08T06:30:18.7826665Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallOpenBrainPrompted1255845941\001\sources\default.toml
2026-04-08T06:30:18.7828793Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallOpenBrainEnvRefs979265822\001\daemon.toml
2026-04-08T06:30:18.7831630Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallOpenBrainEnvRefs979265822\001\destinations\openbrain.toml
2026-04-08T06:30:18.7833266Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallOpenBrainEnvRefs979265822\001\sources\default.toml
2026-04-08T06:30:18.7834699Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallOpenWebUIProfile3751994869\001\daemon.toml
2026-04-08T06:30:18.7836182Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallOpenWebUIProfile3751994869\001\destinations\sqlite.toml
2026-04-08T06:30:18.7838068Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallOpenWebUIProfile3751994869\001\sources\default.toml
2026-04-08T06:30:18.7840147Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallOpenWebUIProfile3751994869\001\sources\openwebui.toml
2026-04-08T06:30:18.7842112Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallOpenWebUIProfile3751994869\001\examples\openwebui-provider.json
2026-04-08T06:30:18.7843666Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallOAuthTemplateCaddy280707686\001\daemon.toml
2026-04-08T06:30:18.7845071Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallOAuthTemplateCaddy280707686\001\destinations\sqlite.toml
2026-04-08T06:30:18.7846451Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallOAuthTemplateCaddy280707686\001\sources\default.toml
2026-04-08T06:30:18.7850793Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallOAuthTemplateCaddy280707686\001\examples\oauth\Caddyfile
2026-04-08T06:30:18.7852353Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallOAuthTemplateTraefik1815912066\001\daemon.toml
2026-04-08T06:30:18.7853885Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallOAuthTemplateTraefik1815912066\001\destinations\sqlite.toml
2026-04-08T06:30:18.7856110Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallOAuthTemplateTraefik1815912066\001\sources\default.toml
2026-04-08T06:30:18.7857742Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallOAuthTemplateTraefik1815912066\001\examples\oauth\traefik.yml
2026-04-08T06:30:18.7859277Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallOAuthTemplateUnknown1268610797\001\daemon.toml
2026-04-08T06:30:18.7861313Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallOAuthTemplateUnknown1268610797\001\destinations\sqlite.toml
2026-04-08T06:30:18.7862907Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallOAuthTemplateUnknown1268610797\001\sources\default.toml
2026-04-08T06:30:18.7864384Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallRefusesWithoutForce698876575\001\daemon.toml
2026-04-08T06:30:18.7865835Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallRefusesWithoutForce698876575\001\destinations\sqlite.toml
2026-04-08T06:30:18.7867310Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallRefusesWithoutForce698876575\001\sources\default.toml
2026-04-08T06:30:18.7868663Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallForceOverwrites267304601\001\daemon.toml
2026-04-08T06:30:18.7870052Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallForceOverwrites267304601\001\destinations\sqlite.toml
2026-04-08T06:30:18.7871476Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallForceOverwrites267304601\001\sources\default.toml
2026-04-08T06:30:18.7872752Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallForceOverwrites267304601\001\daemon.toml
2026-04-08T06:30:18.7874062Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallForceOverwrites267304601\001\destinations\sqlite.toml
2026-04-08T06:30:18.7875395Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallForceOverwrites267304601\001\sources\default.toml
2026-04-08T06:30:18.7876707Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallSimpleModeNextSteps1223912899\001\daemon.toml
2026-04-08T06:30:18.7879054Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallSimpleModeNextSteps1223912899\001\destinations\sqlite.toml
2026-04-08T06:30:18.7880559Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallSimpleModeNextSteps1223912899\001\sources\default.toml
2026-04-08T06:30:18.7882302Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallBalancedModeNextSteps1258017743\001\daemon.toml
2026-04-08T06:30:18.7883625Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallBalancedModeNextSteps1258017743\001\destinations\sqlite.toml
2026-04-08T06:30:18.7886607Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallBalancedModeNextSteps1258017743\001\sources\default.toml
2026-04-08T06:30:18.7888594Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallNeverSilentsqlite-balanced880204663\001\daemon.toml
2026-04-08T06:30:18.7890225Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallNeverSilentsqlite-balanced880204663\001\destinations\sqlite.toml
2026-04-08T06:30:18.7891907Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallNeverSilentsqlite-balanced880204663\001\sources\default.toml
2026-04-08T06:30:18.7893332Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallNeverSilentsqlite-simple2136485405\001\daemon.toml
2026-04-08T06:30:18.7894887Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallNeverSilentsqlite-simple2136485405\001\destinations\sqlite.toml
2026-04-08T06:30:18.7896498Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallNeverSilentsqlite-simple2136485405\001\sources\default.toml
2026-04-08T06:30:18.7898764Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallNeverSilentsqlite-safe3305420845\001\daemon.toml
2026-04-08T06:30:18.7900359Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallNeverSilentsqlite-safe3305420845\001\destinations\sqlite.toml
2026-04-08T06:30:18.7902302Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallNeverSilentsqlite-safe3305420845\001\sources\default.toml
2026-04-08T06:30:18.7903997Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestWritePostgresDestination3674760699\001\destinations\postgres.toml
2026-04-08T06:30:18.7905604Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestWriteOpenBrainDestination742962286\001\destinations\openbrain.toml
2026-04-08T06:30:18.7907260Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestWriteOpenWebUIProviderExample2711378810\001\examples\openwebui-provider.json
2026-04-08T06:30:18.7908857Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestAllProfilesGenerateValidTOMLsqlite_default3419539476\001\daemon.toml
2026-04-08T06:30:18.7910407Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestAllProfilesGenerateValidTOMLsqlite_default3419539476\001\destinations\sqlite.toml
2026-04-08T06:30:18.7912106Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestAllProfilesGenerateValidTOMLsqlite_default3419539476\001\sources\default.toml
2026-04-08T06:30:18.7913851Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestAllProfilesGenerateValidTOMLpostgres_with_DSN1146280374\001\daemon.toml
2026-04-08T06:30:18.7915551Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestAllProfilesGenerateValidTOMLpostgres_with_DSN1146280374\001\destinations\postgres.toml
2026-04-08T06:30:18.7917244Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestAllProfilesGenerateValidTOMLpostgres_with_DSN1146280374\001\sources\default.toml
2026-04-08T06:30:18.7919569Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestAllProfilesGenerateValidTOMLopenbrain_with_credentials3117815499\001\daemon.toml
2026-04-08T06:30:18.7921939Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestAllProfilesGenerateValidTOMLopenbrain_with_credentials3117815499\001\destinations\openbrain.toml
2026-04-08T06:30:18.7923882Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestAllProfilesGenerateValidTOMLopenbrain_with_credentials3117815499\001\sources\default.toml
2026-04-08T06:30:18.7925593Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestAllProfilesGenerateValidTOMLopenwebui_profile901772485\001\daemon.toml
2026-04-08T06:30:18.7927607Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestAllProfilesGenerateValidTOMLopenwebui_profile901772485\001\destinations\sqlite.toml
2026-04-08T06:30:18.7929431Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestAllProfilesGenerateValidTOMLopenwebui_profile901772485\001\sources\default.toml
2026-04-08T06:30:18.7931264Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestAllProfilesGenerateValidTOMLopenwebui_profile901772485\001\sources\openwebui.toml
2026-04-08T06:30:18.7933320Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestAllProfilesGenerateValidTOMLopenwebui_profile901772485\001\examples\openwebui-provider.json
2026-04-08T06:30:18.7935168Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestAllProfilesGenerateValidTOMLcaddy_oauth_template2800680082\001\daemon.toml
2026-04-08T06:30:18.7937391Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestAllProfilesGenerateValidTOMLcaddy_oauth_template2800680082\001\destinations\sqlite.toml
2026-04-08T06:30:18.7939828Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestAllProfilesGenerateValidTOMLcaddy_oauth_template2800680082\001\sources\default.toml
2026-04-08T06:30:18.7941661Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestAllProfilesGenerateValidTOMLcaddy_oauth_template2800680082\001\examples\oauth\Caddyfile
2026-04-08T06:30:18.7943400Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestAllProfilesGenerateValidTOMLtraefik_oauth_template1865886980\001\daemon.toml
2026-04-08T06:30:18.7945737Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestAllProfilesGenerateValidTOMLtraefik_oauth_template1865886980\001\destinations\sqlite.toml
2026-04-08T06:30:18.7947558Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestAllProfilesGenerateValidTOMLtraefik_oauth_template1865886980\001\sources\default.toml
2026-04-08T06:30:18.7949525Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestAllProfilesGenerateValidTOMLtraefik_oauth_template1865886980\001\examples\oauth\traefik.yml
2026-04-08T06:30:18.7951361Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallRespectsConfigDirFlagOverridesEnv1715954178\001\daemon.toml
2026-04-08T06:30:18.7953050Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallRespectsConfigDirFlagOverridesEnv1715954178\001\destinations\sqlite.toml
2026-04-08T06:30:18.7954863Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallRespectsConfigDirFlagOverridesEnv1715954178\001\sources\default.toml
2026-04-08T06:30:18.7956527Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallRespectsConfigDirEnvOverridesDefault3850731909\001\daemon.toml
2026-04-08T06:30:18.7958295Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallRespectsConfigDirEnvOverridesDefault3850731909\001\destinations\sqlite.toml
2026-04-08T06:30:18.7960422Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallRespectsConfigDirEnvOverridesDefault3850731909\001\sources\default.toml
2026-04-08T06:30:18.7961881Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallOAuthIssuer273560471\001\daemon.toml
2026-04-08T06:30:18.7964702Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallOAuthIssuer273560471\001\destinations\sqlite.toml
2026-04-08T06:30:18.7966122Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallOAuthIssuer273560471\001\sources\default.toml
2026-04-08T06:30:18.7967441Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallNoOAuthByDefault113061749\001\daemon.toml
2026-04-08T06:30:18.7968859Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallNoOAuthByDefault113061749\001\destinations\sqlite.toml
2026-04-08T06:30:18.7970229Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestDoInstallNoOAuthByDefault113061749\001\sources\default.toml
2026-04-08T06:30:18.7971315Z --- FAIL: TestBuildDaemonTOML_IncludesAuditSection (0.00s)
2026-04-08T06:30:18.7972499Z     install_test.go:1030: audit log path should not contain tilde reference
2026-04-08T06:30:18.7973786Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestBuildSourceTOML_MatchesInstallExamplePayload210367314\001\daemon.toml
2026-04-08T06:30:18.7975851Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestBuildSourceTOML_MatchesInstallExamplePayload210367314\001\destinations\sqlite.toml
2026-04-08T06:30:18.7977603Z   create: C:\Users\RUNNER~1\AppData\Local\Temp\TestBuildSourceTOML_MatchesInstallExamplePayload210367314\001\sources\default.toml
2026-04-08T06:30:18.7978711Z FAIL
2026-04-08T06:30:18.7979507Z FAIL	github.com/BubbleFish-Nexus/cmd/bubblefish	7.615s
2026-04-08T06:30:32.7242137Z ok  	github.com/BubbleFish-Nexus/internal/audit	21.146s
2026-04-08T06:30:32.7243404Z ok  	github.com/BubbleFish-Nexus/internal/backup	0.553s
2026-04-08T06:30:32.7245670Z ok  	github.com/BubbleFish-Nexus/internal/bench	0.051s
2026-04-08T06:30:32.7249298Z ok  	github.com/BubbleFish-Nexus/internal/cache	0.054s
2026-04-08T06:30:32.7250979Z ok  	github.com/BubbleFish-Nexus/internal/config	0.196s
2026-04-08T06:30:56.7178308Z ok  	github.com/BubbleFish-Nexus/internal/daemon	32.741s
2026-04-08T06:30:56.7196806Z ok  	github.com/BubbleFish-Nexus/internal/demo	0.632s
2026-04-08T06:30:56.7198324Z ok  	github.com/BubbleFish-Nexus/internal/destination	5.841s
2026-04-08T06:30:56.7199667Z ok  	github.com/BubbleFish-Nexus/internal/doctor	0.060s
2026-04-08T06:30:56.7200737Z ok  	github.com/BubbleFish-Nexus/internal/embedding	5.100s
2026-04-08T06:30:56.7246891Z ok  	github.com/BubbleFish-Nexus/internal/eventsink	1.441s
2026-04-08T06:30:56.7249455Z ok  	github.com/BubbleFish-Nexus/internal/firewall	0.075s
2026-04-08T06:30:56.7250802Z ok  	github.com/BubbleFish-Nexus/internal/fsutil	0.048s
2026-04-08T06:30:56.7252227Z ok  	github.com/BubbleFish-Nexus/internal/hotreload	0.133s
2026-04-08T06:30:56.7253878Z ok  	github.com/BubbleFish-Nexus/internal/idempotency	0.024s
2026-04-08T06:30:56.7255314Z ok  	github.com/BubbleFish-Nexus/internal/jwtauth	0.402s
2026-04-08T06:30:56.7257900Z ok  	github.com/BubbleFish-Nexus/internal/lint	0.120s
2026-04-08T06:30:56.7259027Z ok  	github.com/BubbleFish-Nexus/internal/mcp	0.081s
2026-04-08T06:30:56.7260529Z ok  	github.com/BubbleFish-Nexus/internal/metrics	0.040s
2026-04-08T06:31:00.0300507Z ok  	github.com/BubbleFish-Nexus/internal/oauth	4.562s
2026-04-08T06:31:00.0301928Z ok  	github.com/BubbleFish-Nexus/internal/policy	0.077s
2026-04-08T06:31:00.0302916Z ok  	github.com/BubbleFish-Nexus/internal/projection	0.034s
2026-04-08T06:31:00.4102846Z ok  	github.com/BubbleFish-Nexus/internal/query	0.731s
2026-04-08T06:31:00.8270565Z ok  	github.com/BubbleFish-Nexus/internal/queue	0.090s
2026-04-08T06:31:01.0037384Z ok  	github.com/BubbleFish-Nexus/internal/securitylog	0.097s
2026-04-08T06:31:01.6153596Z ok  	github.com/BubbleFish-Nexus/internal/signing	0.287s
2026-04-08T06:31:01.6154554Z ok  	github.com/BubbleFish-Nexus/internal/tray	0.031s
2026-04-08T06:31:01.6188455Z ?   	github.com/BubbleFish-Nexus/internal/version	[no test files]
2026-04-08T06:31:02.2559621Z ok  	github.com/BubbleFish-Nexus/internal/vizpipe	0.188s
2026-04-08T06:31:03.9437906Z ok  	github.com/BubbleFish-Nexus/internal/wal	1.597s
2026-04-08T06:31:03.9439558Z ok  	github.com/BubbleFish-Nexus/internal/web	0.049s
2026-04-08T06:31:03.9440403Z FAIL
2026-04-08T06:31:04.3948540Z ##[error]Process completed with exit code 1.
2026-04-08T06:31:04.4178546Z Post job cleanup.
2026-04-08T06:31:04.7097962Z [command]"C:\Program Files\Git\bin\git.exe" version
2026-04-08T06:31:04.7512749Z git version 2.53.0.windows.2
2026-04-08T06:31:04.7701350Z Temporarily overriding HOME='D:\a\_temp\4994d512-a10d-4be9-bc29-3d772b84a08e' before making global git config changes
2026-04-08T06:31:04.7744545Z Adding repository directory to the temporary git global config as a safe directory
2026-04-08T06:31:04.7770386Z [command]"C:\Program Files\Git\bin\git.exe" config --global --add safe.directory D:\a\BubbleFish-Nexus\BubbleFish-Nexus
2026-04-08T06:31:04.8192826Z [command]"C:\Program Files\Git\bin\git.exe" config --local --name-only --get-regexp core\.sshCommand
2026-04-08T06:31:04.8632572Z [command]"C:\Program Files\Git\bin\git.exe" submodule foreach --recursive "sh -c \"git config --local --name-only --get-regexp 'core\.sshCommand' && git config --local --unset-all 'core.sshCommand' || :\""
2026-04-08T06:31:05.6483103Z [command]"C:\Program Files\Git\bin\git.exe" config --local --name-only --get-regexp http\.https\:\/\/github\.com\/\.extraheader
2026-04-08T06:31:05.6768733Z http.https://github.com/.extraheader
2026-04-08T06:31:05.6858578Z [command]"C:\Program Files\Git\bin\git.exe" config --local --unset-all http.https://github.com/.extraheader
2026-04-08T06:31:05.7185221Z [command]"C:\Program Files\Git\bin\git.exe" submodule foreach --recursive "sh -c \"git config --local --name-only --get-regexp 'http\.https\:\/\/github\.com\/\.extraheader' && git config --local --unset-all 'http.https://github.com/.extraheader' || :\""
2026-04-08T06:31:06.2872200Z [command]"C:\Program Files\Git\bin\git.exe" config --local --name-only --get-regexp ^includeIf\.gitdir:
2026-04-08T06:31:06.3205333Z [command]"C:\Program Files\Git\bin\git.exe" submodule foreach --recursive "git config --local --show-origin --name-only --get-regexp remote.origin.url"
2026-04-08T06:31:06.9113234Z Cleaning up orphan processes
2026-04-08T06:31:06.9421767Z ##[warning]Node.js 20 actions are deprecated. The following actions are running on Node.js 20 and may not work as expected: actions/checkout@v4, actions/setup-go@v5. Actions will be forced to run with Node.js 24 by default starting June 2nd, 2026. Node.js 20 will be removed from the runner on September 16th, 2026. Please check if updated versions of these actions are available that support Node.js 24. To opt into Node.js 24 now, set the FORCE_JAVASCRIPT_ACTIONS_TO_NODE24=true environment variable on the runner or in your workflow file. Once Node.js 24 becomes the default, you can temporarily opt out by setting ACTIONS_ALLOW_USE_UNSECURE_NODE_VERSION=true. For more information see: https://github.blog/changelog/2025-09-19-deprecation-of-node-20-on-github-actions-runners/
