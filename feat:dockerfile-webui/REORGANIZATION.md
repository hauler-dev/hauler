# Project Reorganization - v3.3.5

## Changes Made

### Scripts Moved to `/scripts/` Directory

The following maintenance scripts have been moved from the root directory to `/scripts/`:

1. **cleanup.sh** - Development cleanup script
   - Removes backup files and development artifacts
   - Usage: `./scripts/cleanup.sh`

2. **obfuscate.sh** - Manual JavaScript obfuscation
   - Note: Obfuscation is automatic during Docker build
   - Only needed for local testing
   - Usage: `./scripts/obfuscate.sh`

3. **qa-dependencies.sh** - Dependency validation test
   - Validates Docker image dependencies
   - Usage: `./scripts/qa-dependencies.sh`

### Documentation Moved to `/docs/`

**REWRITE_FLAG_EXPLANATION.md** moved to `/docs/`
- Explains how the `--rewrite` flag works in Hauler
- Critical for understanding registry path configuration
- Now accessible at: `docs/REWRITE_FLAG_EXPLANATION.md`

## Benefits

✅ **Cleaner Root Directory** - Only essential files remain in root  
✅ **Better Organization** - Scripts grouped by purpose  
✅ **Improved Discoverability** - Documentation in docs/ folder  
✅ **Consistent Structure** - Follows standard project layout conventions  

## Root Directory Now Contains

```
hauler-ui/
├── backend/              # Application code
├── frontend/             # Application code
├── docs/                 # All documentation
├── tests/                # Test suites
├── scripts/              # Maintenance scripts (NEW)
├── data/                 # Persistent data
├── Dockerfile            # Build configuration
├── docker-compose.yml    # Deployment configuration
├── Makefile              # Build automation
├── README.md             # Main documentation
└── LICENSE               # License file
```

## Migration Notes

If you have any automation or CI/CD pipelines referencing the old script locations, update them:

**Old:**
```bash
./cleanup.sh
./obfuscate.sh
./qa-dependencies.sh
```

**New:**
```bash
./scripts/cleanup.sh
./scripts/obfuscate.sh
./scripts/qa-dependencies.sh
```

**Documentation:**
- Old: `REWRITE_FLAG_EXPLANATION.md`
- New: `docs/REWRITE_FLAG_EXPLANATION.md`

## Next Steps

Consider moving `qa-dependencies.sh` to `/tests/` directory since it's a validation test rather than a maintenance script.
