# Country Code Migration Analysis & Plan

**Date:** 2025-01-XX  
**Analysis Source:** Production Replica Databases

## Executive Summary

Analysis of production replica databases reveals a split-brain situation with country data:
- **Orders service (accounts)**: 6,416 records with full country names need migration
- **Events service (participant)**: 917 records with full country names need migration  
- **Profiles service (users)**: ✅ Already migrated (all codes)
- **Orders service (invoices)**: No country data

## Current State Analysis

### Orders Service - Accounts Table
- **Total records:** 11,298 (non-deleted)
- **Full names:** 6,416 (56.8%)
- **Codes:** 1,071 (9.5%)
- **Empty/Null:** 3,811 (33.7%)

**Top full country names found:**
- Israel: 2,695
- Russia: 718
- United States: 498
- Ukraine: 365
- Italy: 170
- Germany: 162
- Canada: 133
- Mexico: 126
- Turkey: 119
- Lithuania: 92

**Edge cases identified:**
- "USA" (51) and "United States" (498) - both need → 'US'
- "UK" (2) - needs → 'GB'
- "None" (42) - should be set to NULL
- Typos: "Spaine" → 'ES', "Bulgary" → 'BG', "MEXICO" → 'MX'
- Variations: "Australia " (trailing space), "Israel " (trailing space)
- Alternative names: "Ivory Coast" → 'CI', "The Czech Republic" → 'CZ'
- Short forms: "Congo" → 'CG', "Russian" → 'RU'

### Events Service - Participant Table
- **Total records:** 8,000
- **Full names:** 917 (11.5%)
- **Codes:** 5,078 (63.5%)
- **Empty/Null:** 2,005 (25.0%)

**Edge cases identified:**
- "NODATA" (657) - should be set to NULL
- "USA" (256) - needs → 'US'
- "Moldova, Republic of" (4) - needs → 'MD'

**Note:** Events has a FK constraint to `country_list(code)`, so full names shouldn't exist. This indicates a data integrity issue that needs to be fixed.

### Profiles Service - Users Table
- **Status:** ✅ Already migrated
- **Codes:** 8,696 (100% of non-null values)
- **Empty/Null:** 7,570

### Orders Service - Invoices Table
- **Status:** No country data (all empty/null)

## Migration Plan

### Files Created
1. **`country_code_migration_complete.sql`** - Complete migration script with:
   - Transaction safety (BEGIN/COMMIT)
   - Edge case handling
   - Both accounts and participant tables
   - Pre and post-migration verification queries
   - Rollback capability

### Execution Steps

1. **Pre-Migration Verification**
   ```sql
   -- Run verification queries from Step 1 of migration script
   -- Verify counts match expectations
   ```

2. **Backup** (Recommended)
   ```sql
   -- Create backup of affected tables
   CREATE TABLE accounts_backup AS SELECT * FROM accounts;
   CREATE TABLE participant_backup AS SELECT * FROM participant;
   ```

3. **Execute Migration**
   ```sql
   -- Run the migration script
   -- It's wrapped in a transaction, so can rollback if needed
   ```

4. **Post-Migration Verification**
   ```sql
   -- Run verification queries from Step 3
   -- Should show mostly codes, very few (or zero) full names
   ```

5. **Commit or Rollback**
   ```sql
   -- If verification looks good:
   COMMIT;
   
   -- If issues found:
   ROLLBACK;
   ```

## Safety Considerations

✅ **Safe to execute:**
- All updates are idempotent (can be run multiple times)
- Only updates non-deleted records
- Wrapped in transaction (can rollback)
- Handles edge cases and typos

⚠️ **Considerations:**
- "None" and "NODATA" will be set to NULL (verify this is desired)
- Some typos are corrected (e.g., "Spaine" → 'ES')
- Trailing spaces are handled (e.g., "Australia " → 'AU')

## Expected Results

After migration:
- **Accounts:** ~7,487 records with codes (6,416 migrated + 1,071 existing)
- **Participant:** ~5,995 records with codes (917 migrated + 5,078 existing)
- **Remaining full names:** Should be zero or very few (unmapped countries)

## Verification Queries

After migration, run these to verify success:

```sql
-- Should show mostly codes, very few full names
SELECT 
    CASE 
        WHEN LENGTH("Country") = 2 AND "Country" ~ '^[A-Z]{2}$' THEN 'Code'
        WHEN LENGTH("Country") > 2 THEN 'Full Name'
        ELSE 'Other'
    END as type,
    COUNT(*) as count
FROM accounts
WHERE deleted_at IS NULL
GROUP BY type;

-- List any remaining full names (should be empty)
SELECT DISTINCT "Country", COUNT(*) 
FROM accounts 
WHERE LENGTH("Country") > 2 AND deleted_at IS NULL
GROUP BY "Country";
```

## Notes

- The original `country_code_update_statements.sql` file only handles accounts table and doesn't include edge cases
- The new `country_code_migration_complete.sql` is production-ready and handles all identified issues
- Events service has a FK constraint violation (917 full names despite constraint) - this migration will fix it

