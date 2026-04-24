# Warded CLI Release Notes

## v0.3.0

### Major Changes:

Renamed activate subcommand to new across the CLI and documentation
Moved configuration directory to data directory (configDir → dataDir) for more accurate naming
Changed new subcommand from blocking to non-blocking mode; use serve status subcommand to check and update activation status instead of polling
### Improvements:

Enhanced CLI output format with clearer hints, warnings, and error messages for both users and bots
Updated --domain flag help text to use fully qualified domain name format consistently
### Bug Fixes:

Fixed display logic issue in status subcommand
### Chores:

Optimized install script download logic
Cleaned up legacy deprecated code