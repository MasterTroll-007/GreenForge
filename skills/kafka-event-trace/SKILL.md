# Kafka Event Trace

Skill for tracing Kafka event flows through the system.

## Trigger
User asks about Kafka event flow, topic routing, or message processing pipeline.

## Steps
1. Use codebase index to find all producers/consumers for the topic
2. Map the complete flow: producer → topic → consumer → processing → downstream
3. Identify message types (DTOs/events) used
4. Check error handling and DLT (Dead Letter Topic) configuration
5. Show the complete event flow diagram

## Tools Used
- `kafka_mapper`: map_topics, trace_event, list_listeners
- `spring_analyzer`: list_beans (for Kafka-related beans)
- `file`: file_read (for Kafka configuration)
