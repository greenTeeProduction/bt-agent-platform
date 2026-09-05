#!/bin/bash
export PATH=$PATH:/usr/local/go/bin
cd /home/nico/go-bt-evolve

# Step 1: Get fitness
FITNESS=$(echo '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"bt_get_fitness","arguments":{}}}' | timeout 15 ./bin/bt-agent 2>/dev/null)
if [ $? -ne 0 ] || [ -z "$FITNESS" ]; then
    echo "MCP unavailable"
    exit 1
fi

# Parse fitness - write to temp file to avoid pipe
echo "$FITNESS" > /tmp/bt_fitness.json
python3 -c "
import sys,json
with open('/tmp/bt_fitness.json') as f:
    d=json.load(f)
print('FITNESS_RESULT:', d.get('result',{}))
" 2>/dev/null

# Step 2: Try evolve
EVOLVED=$(echo '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"bt_evolve","arguments":{}}}' | timeout 30 ./bin/bt-agent 2>/dev/null)
if [ $? -ne 0 ] || [ -z "$EVOLVED" ]; then
    echo "evolve unavailable"
    exit 1
fi

echo "$EVOLVED" > /tmp/bt_evolved.json
python3 -c "
import sys,json
with open('/tmp/bt_evolved.json') as f:
    d=json.load(f)
r = json.loads(d['result']['content'][0]['text'])
print('EVOLVED_RESULT:', r.get('evolved','?'))
print('MUTATIONS:', json.dumps(r.get('mutations',[]), indent=2))
" 2>/dev/null
