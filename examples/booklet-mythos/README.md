# Booklet: Mythos

This is a sample game project ("Booklet") built on the Bindery platform.

## Structure

- `modules/`: Custom game engine modules (e.g. spell-engine).
- `realms/`: Game definitions and world templates.
- `shards/`: Runtime configuration for world instances.
- `contracts/`: Game-specific capability contracts.

## Usage

1. Ensure Bindery Core is installed.
2. Apply the booklet manifest:
   ```bash
   kubectl apply -f realms/booklet.yaml
   ```
