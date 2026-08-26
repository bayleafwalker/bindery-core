# bindery-relay/v1 wire contract

The relay envelope is deliberately opaque after authentication. All integer
fields use network byte order. The fixed header is 98 bytes:

| offset | size | field |
| ---: | ---: | --- |
| 0 | 4 | magic `BRLY` |
| 4 | 1 | protocol version `1` |
| 5 | 1 | packet type (`data`, `register`, `heartbeat`) |
| 6 | 2 | reserved flags, zero |
| 8 | 16 | allocation UUID bytes |
| 24 | 16 | sender opaque client UUID bytes |
| 40 | 16 | recipient opaque client UUID bytes |
| 56 | 8 | sender sequence |
| 64 | 2 | payload length |
| 66 | 32 | HMAC-SHA256 |

The MAC covers bytes `0..65` followed by the opaque payload. A sender MAC is
verified with that client's transport key. The relay emits the same envelope
with the recipient's key, so clients never share transport credentials. The
default complete datagram limit is 1,400 bytes; implementations may lower it
per allocation but must reject oversize input before forwarding.

The forwarding hot path owns no broker, database, object-store, or telemetry
dependency. Telemetry is best-effort and non-blocking; relay failure is
reported explicitly to the control plane.

