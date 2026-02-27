#!/bin/bash
# Claude Code API Proxy - bridges GreenForge Docker container to Claude CLI
# Usage: bash claude-proxy.sh
# Runs on port 18790, accepts Anthropic-compatible /v1/messages requests

PORT=18790
echo "Claude Code API Proxy starting on port $PORT..."
echo "This bridges GreenForge to your local Claude Code subscription."
echo ""

# Simple HTTP server using socat + claude CLI
while true; do
  # Use a temp file for the response
  RESP=$(mktemp)

  # Listen for one connection
  socat TCP-LISTEN:$PORT,reuseaddr,fork SYSTEM:"
    read -r REQUEST_LINE
    METHOD=\$(echo \$REQUEST_LINE | cut -d' ' -f1)

    # Read headers until empty line
    CONTENT_LENGTH=0
    while read -r HEADER; do
      HEADER=\$(echo \$HEADER | tr -d '\r')
      [ -z \"\$HEADER\" ] && break
      case \$HEADER in
        Content-Length:*|content-length:*) CONTENT_LENGTH=\$(echo \$HEADER | cut -d':' -f2 | tr -d ' ');;
      esac
    done

    # Read body
    BODY=''
    if [ \$CONTENT_LENGTH -gt 0 ]; then
      BODY=\$(head -c \$CONTENT_LENGTH)
    fi

    if [ \"\$METHOD\" = 'POST' ]; then
      # Extract messages from JSON body and format for claude CLI
      PROMPT=\$(echo \$BODY | jq -r '.messages | map(select(.role==\"user\")) | last .content' 2>/dev/null)
      SYSTEM=\$(echo \$BODY | jq -r '.system // empty' 2>/dev/null)
      MODEL=\$(echo \$BODY | jq -r '.model // empty' 2>/dev/null)
      MAX_TOKENS=\$(echo \$BODY | jq -r '.max_tokens // 4096' 2>/dev/null)

      # Call claude CLI
      CLAUDE_ARGS='-p --output-format json'
      if [ -n \"\$MODEL\" ]; then
        CLAUDE_ARGS=\"\$CLAUDE_ARGS --model \$MODEL\"
      fi
      if [ -n \"\$SYSTEM\" ]; then
        CLAUDE_ARGS=\"\$CLAUDE_ARGS --system-prompt \\\"\$SYSTEM\\\"\"
      fi

      RESULT=\$(echo \"\$PROMPT\" | claude \$CLAUDE_ARGS 2>/dev/null)

      # Format as Anthropic Messages API response
      CONTENT=\$(echo \"\$RESULT\" | jq -r '.result // .content // .' 2>/dev/null)

      RESPONSE=\$(jq -n --arg content \"\$CONTENT\" --arg model \"\${MODEL:-claude-sonnet-4-6}\" '{
        id: \"msg_proxy\",
        type: \"message\",
        role: \"assistant\",
        model: \$model,
        content: [{type: \"text\", text: \$content}],
        stop_reason: \"end_turn\",
        usage: {input_tokens: 0, output_tokens: 0}
      }')

      echo \"HTTP/1.1 200 OK\"
      echo \"Content-Type: application/json\"
      echo \"\"
      echo \"\$RESPONSE\"
    else
      echo 'HTTP/1.1 200 OK'
      echo 'Content-Type: application/json'
      echo ''
      echo '{\"status\":\"ok\",\"proxy\":\"claude-code\"}'
    fi
  " 2>/dev/null
done
