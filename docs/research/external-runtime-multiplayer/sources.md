# Primary-source baseline

These sources informed the boundary between a CnCNet-like backend and the
client-side compatibility work. They are evidence for the comparison, not a
dependency commitment.

## CnCNet server and transport

- [CnCNet server startup](https://github.com/CnCNet/cncnet-server/blob/master/Program.cs): starts V2/V3 tunnel services and peer-to-peer/NAT traversal listeners.
- [Tunnel V2 implementation](https://github.com/CnCNet/cncnet-server/blob/master/CnCNet/Net/Tunnel/TunnelV2.cs): client-ID allocation, endpoint mapping, heartbeat expiry and opaque UDP forwarding.
- [Tunnel V3 implementation](https://github.com/CnCNet/cncnet-server/blob/master/CnCNet/Net/Tunnel/TunnelV3.cs): newer relay protocol implementation.
- [Server options](https://github.com/CnCNet/cncnet-server/blob/master/Options.cs): capacity, rate/packet limits, master announcement and P2P settings.
- [Container deployment](https://github.com/CnCNet/cncnet-docker-dotnetcore-tunnel/blob/main/README.md): current ports and deployment shape for the .NET tunnel server.

## Client and game adapter

- [Tunnel selection/refresh](https://github.com/CnCNet/xna-cncnet-client/blob/develop/DXMainClient/Domain/Multiplayer/CnCNet/TunnelHandler.cs): tunnel discovery, caching, filtering and latency probes.
- [Game loading/launch configuration](https://github.com/CnCNet/xna-cncnet-client/blob/develop/DXMainClient/DXGUI/Multiplayer/GameLoadingLobbyBase.cs): writes `spawn.ini`, starts the game and parses statistics after exit.
- [Process launch](https://github.com/CnCNet/xna-cncnet-client/blob/develop/ClientGUI/GameProcessLogic.cs): launches the selected game executable with the spawn mode.
- [YR++ statistics hooks](https://github.com/CnCNet/yrpp-spawner/blob/master/src/Spawner/Statistics.cpp): hooks statistics generation and writes a post-match dump with additional match fields.

## Lobby comparison

- [Room service](https://github.com/CnCNet/cncnet-websocket-server/blob/develop/src/services/RoomService.ts): in-memory rooms, membership and deletion.
- [Room controller](https://github.com/CnCNet/cncnet-websocket-server/blob/develop/src/controllers/RoomController.ts): room creation/join/chat/options broadcast behavior that is deliberately outside the first Bindery target.

## License boundary

- [CnCNet server license](https://github.com/CnCNet/cncnet-server/blob/master/LICENSE)
- [XNA CnCNet client license](https://github.com/CnCNet/xna-cncnet-client/blob/develop/LICENSE)

Both inspected repositories use GPLv3. Reuse, derivation or wire-compatible
clean-room implementation should be a deliberate project/legal decision. A
separately deployed baseline provider is the least committal research starting
point.

