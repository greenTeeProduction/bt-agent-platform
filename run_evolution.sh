#!/bin/bash
# Tree evolution cycle via JSON-RPC to bt-agent
cd /home/nico/go-bt-evolve || exit 1

# Step 1: Get fitness
echo '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"bt_get_fitness","arguments":{}}}' | timeout 15 ./bt-agent 2>/dev/null > /tmp/bt_fitness_out.json
python3 -c "
import sys, json
try:
    with open('/tmp/bt_fitness_out.json') as f:
        d = json.load(f)
    result = d.get('result', {})
    if isinstance(result, dict) and 'content' in result:
        print('FITNESS:', result['content'])
    else:
        print('FITNESS:', result)
except Exception as e:
    print('FITNESS_ERROR:', e)
" 2>&1

# Step 2: Try evolve if we got fitness
echo '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"bt_evolve","arguments":{}}}' | timeout 30 ./bt-agent 2>/dev/null > /tmp/bt_evolve_out.json
python3 -c "
import sys, json
try:
    with open('/tmp/bt_evolve_out.json') as f:
        d = json.load(f)
    result = d.get('result', {})
    if isinstance(result, dict) and 'content' in result:
        content = result['content']
        if isinstance(content, list) and len(content) > 0:
            try:
                r = json.loads(content[0].get('text', '{}'))
                print('EVOLVED:', r)
            except:
                print('EVOLVED_CONTENT:', content[0])
        else:
            print('EVOLVED:', content)
    else:
        print('EVOLVED:', result)
except Exception as e:
    print('EVOLVE_ERROR:', e)
" 2>&1
