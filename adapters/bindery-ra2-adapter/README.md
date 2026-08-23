# Bindery RA2/YR adapter

GPL-3.0-or-later Windows adapter boundary for the Bindery external-runtime
research experiment. The repository contains client-owned control-plane and
launch plumbing only. It does not contain Red Alert 2/Yuri's Revenge
executables, maps, registry exports, proprietary assets, or redistributed game
files.

The adapter supports two explicit transport providers:

- `bindery-native`: the `bindery-relay/v1` opaque relay contract;
- `cncnet-baseline`: an unchanged CnCNet tunnel deployment selected for the
  comparison baseline.

The spawner/injection boundary is represented by `ISpawnerBoundary`; the
adapter never copies or embeds proprietary game code. A Windows CI runner must
perform the real spawner build and field acceptance after the upstream notices
and revisions are reviewed.

## Local checks

```powershell
dotnet build src/Bindery.Ra2.Adapter/Bindery.Ra2.Adapter.csproj --configuration Release
dotnet test
```

