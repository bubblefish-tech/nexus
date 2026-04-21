\# ============================================================

\# A2A SELF-REGISTRATION — OpenClaw Side (Linux CC)

\# ============================================================

\# Modifies the running a2a-receiver extension to self-register

\# with Nexus on every gateway boot.

\# ============================================================



\# 1. Verify the extension exists

echo "========== PRE-CHECK =========="

ls -la \~/.openclaw/extensions/a2a-receiver/index.js || {

&#x20;   echo "FATAL: a2a-receiver extension not found"

&#x20;   exit 1

}



\# 2. Back up the current version

cp \~/.openclaw/extensions/a2a-receiver/index.js \\

&#x20;  \~/.openclaw/extensions/a2a-receiver/index.js.bak.$(date +%s)

echo "Backed up existing index.js"



\# 3. Read the current file so you understand its structure

echo ""

echo "========== CURRENT FILE (first 50 lines) =========="

head -50 \~/.openclaw/extensions/a2a-receiver/index.js



\# 4. Check what env vars are already set

echo ""

echo "========== ENV VARS =========="

echo "A2A\_SHARED\_SECRET=${A2A\_SHARED\_SECRET:-(NOT SET)}"

echo "OPENCLAW\_TOKEN=${OPENCLAW\_TOKEN:-(NOT SET)}"

echo "NEXUS\_URL=${NEXUS\_URL:-(NOT SET)}"



\# 5. Set NEXUS\_URL if not already in bashrc

echo ""

echo "========== SETTING NEXUS\_URL =========="

if grep -q "NEXUS\_URL" \~/.bashrc; then

&#x20;   echo "NEXUS\_URL already in \~/.bashrc"

&#x20;   grep "NEXUS\_URL" \~/.bashrc

else

&#x20;   echo 'export NEXUS\_URL="http://localhost:8080"' >> \~/.bashrc

&#x20;   export NEXUS\_URL="http://localhost:8080"

&#x20;   echo "Added NEXUS\_URL=http://localhost:8080 to \~/.bashrc"

fi



\# 6. Show the full current index.js

echo ""

echo "========== FULL CURRENT index.js =========="

cat \~/.openclaw/extensions/a2a-receiver/index.js



echo ""

echo "========== PRE-CHECK COMPLETE =========="

echo "CC: Now read the file above, then add the register() and"

echo "detectMyURL() functions. Do NOT rewrite the whole file —"

echo "add to what exists. The activate/init function needs a"

echo "setTimeout(() => register(gateway), 1000) call after"

echo "routes are registered."

