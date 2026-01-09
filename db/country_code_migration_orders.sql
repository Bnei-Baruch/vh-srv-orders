-- ============================================================================
-- COUNTRY CODE MIGRATION SCRIPT - ORDERS DATABASE
-- ============================================================================
-- Purpose: Convert full country names to ISO 2-letter country codes
-- Table: accounts
-- Database: orders
-- Based on: Production replica analysis (2025-01-XX)
-- 
-- IMPORTANT: This script updates ALL records including deleted ones
-- 
-- Findings:
-- - Orders accounts: 6,416 records with full names need migration
-- ============================================================================

-- STEP 1: PRE-MIGRATION VERIFICATION
-- ============================================================================
-- Run these queries FIRST to verify current state matches expectations:

-- 1.1: Orders - Accounts distribution (ALL records including deleted)
SELECT 
    CASE 
        WHEN LENGTH("Country") = 2 AND "Country" ~ '^[A-Z]{2}$' THEN 'Code (2-letter)'
        WHEN LENGTH("Country") > 2 THEN 'Full Name'
        WHEN "Country" IS NULL OR "Country" = '' THEN 'Empty/Null'
        ELSE 'Other'
    END as country_type,
    COUNT(*) as count
FROM accounts
GROUP BY country_type
ORDER BY count DESC;

-- 1.2: List all unique full country names
SELECT DISTINCT "Country", COUNT(*) as count
FROM accounts
WHERE LENGTH("Country") > 2 
GROUP BY "Country"
ORDER BY count DESC
LIMIT 50;

-- ============================================================================
-- STEP 2: EXECUTE MIGRATION IN TRANSACTION
-- ============================================================================
-- IMPORTANT: Review verification queries above before proceeding!
-- This transaction can be rolled back if something goes wrong.

BEGIN;

-- ============================================================================
-- ORDERS SERVICE - ACCOUNTS TABLE
-- ============================================================================

-- Handle edge cases and special values FIRST (ALL records including deleted)
UPDATE accounts 
SET "Country" = 'US' 
WHERE "Country" IN ('USA', 'U.S.A.', 'United States of America', 'United States');

UPDATE accounts 
SET "Country" = 'GB' 
WHERE "Country" = 'UK';

-- Handle "None" - set to NULL (or keep as is if you prefer)
UPDATE accounts 
SET "Country" = NULL 
WHERE "Country" = 'None';

-- Handle typos and variations
UPDATE accounts SET "Country" = 'AU' WHERE "Country" IN ('Australia', 'Australia ');
UPDATE accounts SET "Country" = 'IL' WHERE "Country" IN ('Israel', 'Israel ');
UPDATE accounts SET "Country" = 'MX' WHERE "Country" IN ('Mexico', 'MEXICO');
UPDATE accounts SET "Country" = 'RU' WHERE "Country" IN ('Russia', 'Russian', 'Russian Federation');
UPDATE accounts SET "Country" = 'ES' WHERE "Country" IN ('Spain', 'Spaine');
UPDATE accounts SET "Country" = 'BG' WHERE "Country" IN ('Bulgaria', 'Bulgary');
UPDATE accounts SET "Country" = 'CC' WHERE "Country" = 'Cocos Islands';
UPDATE accounts SET "Country" = 'CI' WHERE "Country" IN ('Cote d''Ivoire', 'Cote d Ivoire', 'Ivory Coast');
UPDATE accounts SET "Country" = 'CZ' WHERE "Country" IN ('Czech Republic', 'The Czech Republic');
UPDATE accounts SET "Country" = 'CG' WHERE "Country" = 'Congo';
UPDATE accounts SET "Country" = 'MD' WHERE "Country" IN ('Moldova', 'Moldova, Republic of');

-- Standard country name to code mappings (ALL records including deleted)
UPDATE accounts SET "Country" = 'AD' WHERE "Country" = 'Andorra';
UPDATE accounts SET "Country" = 'AE' WHERE "Country" = 'United Arab Emirates';
UPDATE accounts SET "Country" = 'AF' WHERE "Country" = 'Afghanistan';
UPDATE accounts SET "Country" = 'AG' WHERE "Country" = 'Antigua and Barbuda';
UPDATE accounts SET "Country" = 'AI' WHERE "Country" = 'Anguilla';
UPDATE accounts SET "Country" = 'AL' WHERE "Country" = 'Albania';
UPDATE accounts SET "Country" = 'AM' WHERE "Country" = 'Armenia';
UPDATE accounts SET "Country" = 'AO' WHERE "Country" = 'Angola';
UPDATE accounts SET "Country" = 'AQ' WHERE "Country" = 'Antarctica';
UPDATE accounts SET "Country" = 'AR' WHERE "Country" = 'Argentina';
UPDATE accounts SET "Country" = 'AS' WHERE "Country" = 'American Samoa';
UPDATE accounts SET "Country" = 'AT' WHERE "Country" = 'Austria';
UPDATE accounts SET "Country" = 'AU' WHERE "Country" = 'Australia';
UPDATE accounts SET "Country" = 'AW' WHERE "Country" = 'Aruba';
UPDATE accounts SET "Country" = 'AX' WHERE "Country" = 'Alland Islands';
UPDATE accounts SET "Country" = 'AZ' WHERE "Country" = 'Azerbaijan';
UPDATE accounts SET "Country" = 'BA' WHERE "Country" = 'Bosnia and Herzegovina';
UPDATE accounts SET "Country" = 'BB' WHERE "Country" = 'Barbados';
UPDATE accounts SET "Country" = 'BD' WHERE "Country" = 'Bangladesh';
UPDATE accounts SET "Country" = 'BE' WHERE "Country" = 'Belgium';
UPDATE accounts SET "Country" = 'BF' WHERE "Country" = 'Burkina Faso';
UPDATE accounts SET "Country" = 'BH' WHERE "Country" = 'Bahrain';
UPDATE accounts SET "Country" = 'BI' WHERE "Country" = 'Burundi';
UPDATE accounts SET "Country" = 'BJ' WHERE "Country" = 'Benin';
UPDATE accounts SET "Country" = 'BL' WHERE "Country" = 'Saint Barthelemy';
UPDATE accounts SET "Country" = 'BM' WHERE "Country" = 'Bermuda';
UPDATE accounts SET "Country" = 'BN' WHERE "Country" = 'Brunei Darussalam';
UPDATE accounts SET "Country" = 'BO' WHERE "Country" = 'Bolivia';
UPDATE accounts SET "Country" = 'BR' WHERE "Country" = 'Brazil';
UPDATE accounts SET "Country" = 'BS' WHERE "Country" = 'Bahamas';
UPDATE accounts SET "Country" = 'BT' WHERE "Country" = 'Bhutan';
UPDATE accounts SET "Country" = 'BV' WHERE "Country" = 'Bouvet Island';
UPDATE accounts SET "Country" = 'BW' WHERE "Country" = 'Botswana';
UPDATE accounts SET "Country" = 'BY' WHERE "Country" = 'Belarus';
UPDATE accounts SET "Country" = 'BZ' WHERE "Country" = 'Belize';
UPDATE accounts SET "Country" = 'CA' WHERE "Country" = 'Canada';
UPDATE accounts SET "Country" = 'CD' WHERE "Country" = 'Congo, The Democratic Republic of the';
UPDATE accounts SET "Country" = 'CF' WHERE "Country" = 'Central African Republic';
UPDATE accounts SET "Country" = 'CG' WHERE "Country" = 'Congo, Republic of the';
UPDATE accounts SET "Country" = 'CH' WHERE "Country" = 'Switzerland';
UPDATE accounts SET "Country" = 'CK' WHERE "Country" = 'Cook Islands';
UPDATE accounts SET "Country" = 'CL' WHERE "Country" = 'Chile';
UPDATE accounts SET "Country" = 'CM' WHERE "Country" = 'Cameroon';
UPDATE accounts SET "Country" = 'CN' WHERE "Country" = 'China';
UPDATE accounts SET "Country" = 'CO' WHERE "Country" = 'Colombia';
UPDATE accounts SET "Country" = 'CR' WHERE "Country" = 'Costa Rica';
UPDATE accounts SET "Country" = 'CU' WHERE "Country" = 'Cuba';
UPDATE accounts SET "Country" = 'CV' WHERE "Country" = 'Cape Verde';
UPDATE accounts SET "Country" = 'CW' WHERE "Country" = 'Curacao';
UPDATE accounts SET "Country" = 'CX' WHERE "Country" = 'Christmas Island';
UPDATE accounts SET "Country" = 'CY' WHERE "Country" = 'Cyprus';
UPDATE accounts SET "Country" = 'CZ' WHERE "Country" = 'Czech Republic';
UPDATE accounts SET "Country" = 'DE' WHERE "Country" = 'Germany';
UPDATE accounts SET "Country" = 'DJ' WHERE "Country" = 'Djibouti';
UPDATE accounts SET "Country" = 'DK' WHERE "Country" = 'Denmark';
UPDATE accounts SET "Country" = 'DM' WHERE "Country" = 'Dominica';
UPDATE accounts SET "Country" = 'DO' WHERE "Country" = 'Dominican Republic';
UPDATE accounts SET "Country" = 'DZ' WHERE "Country" = 'Algeria';
UPDATE accounts SET "Country" = 'EC' WHERE "Country" = 'Ecuador';
UPDATE accounts SET "Country" = 'EE' WHERE "Country" = 'Estonia';
UPDATE accounts SET "Country" = 'EG' WHERE "Country" = 'Egypt';
UPDATE accounts SET "Country" = 'EH' WHERE "Country" = 'Western Sahara';
UPDATE accounts SET "Country" = 'ER' WHERE "Country" = 'Eritrea';
UPDATE accounts SET "Country" = 'ES' WHERE "Country" = 'Spain';
UPDATE accounts SET "Country" = 'ET' WHERE "Country" = 'Ethiopia';
UPDATE accounts SET "Country" = 'FI' WHERE "Country" = 'Finland';
UPDATE accounts SET "Country" = 'FJ' WHERE "Country" = 'Fiji';
UPDATE accounts SET "Country" = 'FK' WHERE "Country" = 'Falkland Islands (Malvinas)';
UPDATE accounts SET "Country" = 'FM' WHERE "Country" = 'Micronesia, Federated States of';
UPDATE accounts SET "Country" = 'FO' WHERE "Country" = 'Faroe Islands';
UPDATE accounts SET "Country" = 'FR' WHERE "Country" = 'France';
UPDATE accounts SET "Country" = 'GA' WHERE "Country" = 'Gabon';
UPDATE accounts SET "Country" = 'GB' WHERE "Country" = 'United Kingdom';
UPDATE accounts SET "Country" = 'GD' WHERE "Country" = 'Grenada';
UPDATE accounts SET "Country" = 'GE' WHERE "Country" = 'Georgia';
UPDATE accounts SET "Country" = 'GF' WHERE "Country" = 'French Guiana';
UPDATE accounts SET "Country" = 'GG' WHERE "Country" = 'Guernsey';
UPDATE accounts SET "Country" = 'GH' WHERE "Country" = 'Ghana';
UPDATE accounts SET "Country" = 'GI' WHERE "Country" = 'Gibraltar';
UPDATE accounts SET "Country" = 'GL' WHERE "Country" = 'Greenland';
UPDATE accounts SET "Country" = 'GM' WHERE "Country" = 'Gambia';
UPDATE accounts SET "Country" = 'GN' WHERE "Country" = 'Guinea';
UPDATE accounts SET "Country" = 'GP' WHERE "Country" = 'Guadeloupe';
UPDATE accounts SET "Country" = 'GQ' WHERE "Country" = 'Equatorial Guinea';
UPDATE accounts SET "Country" = 'GR' WHERE "Country" = 'Greece';
UPDATE accounts SET "Country" = 'GS' WHERE "Country" = 'South Georgia and the South Sandwich Islands';
UPDATE accounts SET "Country" = 'GT' WHERE "Country" = 'Guatemala';
UPDATE accounts SET "Country" = 'GU' WHERE "Country" = 'Guam';
UPDATE accounts SET "Country" = 'GW' WHERE "Country" = 'Guinea-Bissau';
UPDATE accounts SET "Country" = 'GY' WHERE "Country" = 'Guyana';
UPDATE accounts SET "Country" = 'HK' WHERE "Country" = 'Hong Kong';
UPDATE accounts SET "Country" = 'HM' WHERE "Country" = 'Heard Island and McDonald Islands';
UPDATE accounts SET "Country" = 'HN' WHERE "Country" = 'Honduras';
UPDATE accounts SET "Country" = 'HR' WHERE "Country" = 'Croatia';
UPDATE accounts SET "Country" = 'HT' WHERE "Country" = 'Haiti';
UPDATE accounts SET "Country" = 'HU' WHERE "Country" = 'Hungary';
UPDATE accounts SET "Country" = 'ID' WHERE "Country" = 'Indonesia';
UPDATE accounts SET "Country" = 'IE' WHERE "Country" = 'Ireland';
UPDATE accounts SET "Country" = 'IL' WHERE "Country" = 'Israel';
UPDATE accounts SET "Country" = 'IM' WHERE "Country" = 'Isle of Man';
UPDATE accounts SET "Country" = 'IN' WHERE "Country" = 'India';
UPDATE accounts SET "Country" = 'IO' WHERE "Country" = 'British Indian Ocean Territory';
UPDATE accounts SET "Country" = 'IQ' WHERE "Country" = 'Iraq';
UPDATE accounts SET "Country" = 'IR' WHERE "Country" = 'Iran, Islamic Republic of';
UPDATE accounts SET "Country" = 'IS' WHERE "Country" = 'Iceland';
UPDATE accounts SET "Country" = 'IT' WHERE "Country" = 'Italy';
UPDATE accounts SET "Country" = 'JE' WHERE "Country" = 'Jersey';
UPDATE accounts SET "Country" = 'JM' WHERE "Country" = 'Jamaica';
UPDATE accounts SET "Country" = 'JO' WHERE "Country" = 'Jordan';
UPDATE accounts SET "Country" = 'JP' WHERE "Country" = 'Japan';
UPDATE accounts SET "Country" = 'KE' WHERE "Country" = 'Kenya';
UPDATE accounts SET "Country" = 'KG' WHERE "Country" = 'Kyrgyzstan';
UPDATE accounts SET "Country" = 'KH' WHERE "Country" = 'Cambodia';
UPDATE accounts SET "Country" = 'KI' WHERE "Country" = 'Kiribati';
UPDATE accounts SET "Country" = 'KM' WHERE "Country" = 'Comoros';
UPDATE accounts SET "Country" = 'KN' WHERE "Country" = 'Saint Kitts and Nevis';
UPDATE accounts SET "Country" = 'KP' WHERE "Country" = 'Korea, Democratic People''s Republic of';
UPDATE accounts SET "Country" = 'KR' WHERE "Country" = 'Korea, Republic of';
UPDATE accounts SET "Country" = 'KW' WHERE "Country" = 'Kuwait';
UPDATE accounts SET "Country" = 'KY' WHERE "Country" = 'Cayman Islands';
UPDATE accounts SET "Country" = 'KZ' WHERE "Country" = 'Kazakhstan';
UPDATE accounts SET "Country" = 'LA' WHERE "Country" = 'Lao People''s Democratic Republic';
UPDATE accounts SET "Country" = 'LB' WHERE "Country" = 'Lebanon';
UPDATE accounts SET "Country" = 'LC' WHERE "Country" = 'Saint Lucia';
UPDATE accounts SET "Country" = 'LI' WHERE "Country" = 'Liechtenstein';
UPDATE accounts SET "Country" = 'LK' WHERE "Country" = 'Sri Lanka';
UPDATE accounts SET "Country" = 'LR' WHERE "Country" = 'Liberia';
UPDATE accounts SET "Country" = 'LS' WHERE "Country" = 'Lesotho';
UPDATE accounts SET "Country" = 'LT' WHERE "Country" = 'Lithuania';
UPDATE accounts SET "Country" = 'LU' WHERE "Country" = 'Luxembourg';
UPDATE accounts SET "Country" = 'LV' WHERE "Country" = 'Latvia';
UPDATE accounts SET "Country" = 'LY' WHERE "Country" = 'Libya';
UPDATE accounts SET "Country" = 'MA' WHERE "Country" = 'Morocco';
UPDATE accounts SET "Country" = 'MC' WHERE "Country" = 'Monaco';
UPDATE accounts SET "Country" = 'ME' WHERE "Country" = 'Montenegro';
UPDATE accounts SET "Country" = 'MF' WHERE "Country" = 'Saint Martin (French part)';
UPDATE accounts SET "Country" = 'MG' WHERE "Country" = 'Madagascar';
UPDATE accounts SET "Country" = 'MH' WHERE "Country" = 'Marshall Islands';
UPDATE accounts SET "Country" = 'MK' WHERE "Country" = 'Macedonia, the Former Yugoslav Republic of';
UPDATE accounts SET "Country" = 'ML' WHERE "Country" = 'Mali';
UPDATE accounts SET "Country" = 'MM' WHERE "Country" = 'Myanmar';
UPDATE accounts SET "Country" = 'MN' WHERE "Country" = 'Mongolia';
UPDATE accounts SET "Country" = 'MO' WHERE "Country" = 'Macao';
UPDATE accounts SET "Country" = 'MP' WHERE "Country" = 'Northern Mariana Islands';
UPDATE accounts SET "Country" = 'MQ' WHERE "Country" = 'Martinique';
UPDATE accounts SET "Country" = 'MR' WHERE "Country" = 'Mauritania';
UPDATE accounts SET "Country" = 'MS' WHERE "Country" = 'Montserrat';
UPDATE accounts SET "Country" = 'MT' WHERE "Country" = 'Malta';
UPDATE accounts SET "Country" = 'MU' WHERE "Country" = 'Mauritius';
UPDATE accounts SET "Country" = 'MV' WHERE "Country" = 'Maldives';
UPDATE accounts SET "Country" = 'MW' WHERE "Country" = 'Malawi';
UPDATE accounts SET "Country" = 'MX' WHERE "Country" = 'Mexico';
UPDATE accounts SET "Country" = 'MY' WHERE "Country" = 'Malaysia';
UPDATE accounts SET "Country" = 'MZ' WHERE "Country" = 'Mozambique';
UPDATE accounts SET "Country" = 'NA' WHERE "Country" = 'Namibia';
UPDATE accounts SET "Country" = 'NC' WHERE "Country" = 'New Caledonia';
UPDATE accounts SET "Country" = 'NE' WHERE "Country" = 'Niger';
UPDATE accounts SET "Country" = 'NF' WHERE "Country" = 'Norfolk Island';
UPDATE accounts SET "Country" = 'NG' WHERE "Country" = 'Nigeria';
UPDATE accounts SET "Country" = 'NI' WHERE "Country" = 'Nicaragua';
UPDATE accounts SET "Country" = 'NL' WHERE "Country" = 'Netherlands';
UPDATE accounts SET "Country" = 'NO' WHERE "Country" = 'Norway';
UPDATE accounts SET "Country" = 'NP' WHERE "Country" = 'Nepal';
UPDATE accounts SET "Country" = 'NR' WHERE "Country" = 'Nauru';
UPDATE accounts SET "Country" = 'NU' WHERE "Country" = 'Niue';
UPDATE accounts SET "Country" = 'NZ' WHERE "Country" = 'New Zealand';
UPDATE accounts SET "Country" = 'OM' WHERE "Country" = 'Oman';
UPDATE accounts SET "Country" = 'PA' WHERE "Country" = 'Panama';
UPDATE accounts SET "Country" = 'PE' WHERE "Country" = 'Peru';
UPDATE accounts SET "Country" = 'PF' WHERE "Country" = 'French Polynesia';
UPDATE accounts SET "Country" = 'PG' WHERE "Country" = 'Papua New Guinea';
UPDATE accounts SET "Country" = 'PH' WHERE "Country" = 'Philippines';
UPDATE accounts SET "Country" = 'PK' WHERE "Country" = 'Pakistan';
UPDATE accounts SET "Country" = 'PL' WHERE "Country" = 'Poland';
UPDATE accounts SET "Country" = 'PM' WHERE "Country" = 'Saint Pierre and Miquelon';
UPDATE accounts SET "Country" = 'PN' WHERE "Country" = 'Pitcairn';
UPDATE accounts SET "Country" = 'PR' WHERE "Country" = 'Puerto Rico';
UPDATE accounts SET "Country" = 'PS' WHERE "Country" = 'Palestine, State of';
UPDATE accounts SET "Country" = 'PT' WHERE "Country" = 'Portugal';
UPDATE accounts SET "Country" = 'PW' WHERE "Country" = 'Palau';
UPDATE accounts SET "Country" = 'PY' WHERE "Country" = 'Paraguay';
UPDATE accounts SET "Country" = 'QA' WHERE "Country" = 'Qatar';
UPDATE accounts SET "Country" = 'RE' WHERE "Country" = 'Reunion';
UPDATE accounts SET "Country" = 'RO' WHERE "Country" = 'Romania';
UPDATE accounts SET "Country" = 'RS' WHERE "Country" = 'Serbia';
UPDATE accounts SET "Country" = 'RU' WHERE "Country" = 'Russian Federation';
UPDATE accounts SET "Country" = 'RW' WHERE "Country" = 'Rwanda';
UPDATE accounts SET "Country" = 'SA' WHERE "Country" = 'Saudi Arabia';
UPDATE accounts SET "Country" = 'SB' WHERE "Country" = 'Solomon Islands';
UPDATE accounts SET "Country" = 'SC' WHERE "Country" = 'Seychelles';
UPDATE accounts SET "Country" = 'SD' WHERE "Country" = 'Sudan';
UPDATE accounts SET "Country" = 'SE' WHERE "Country" = 'Sweden';
UPDATE accounts SET "Country" = 'SG' WHERE "Country" = 'Singapore';
UPDATE accounts SET "Country" = 'SH' WHERE "Country" = 'Saint Helena';
UPDATE accounts SET "Country" = 'SI' WHERE "Country" = 'Slovenia';
UPDATE accounts SET "Country" = 'SJ' WHERE "Country" = 'Svalbard and Jan Mayen';
UPDATE accounts SET "Country" = 'SK' WHERE "Country" = 'Slovakia';
UPDATE accounts SET "Country" = 'SL' WHERE "Country" = 'Sierra Leone';
UPDATE accounts SET "Country" = 'SM' WHERE "Country" = 'San Marino';
UPDATE accounts SET "Country" = 'SN' WHERE "Country" = 'Senegal';
UPDATE accounts SET "Country" = 'SO' WHERE "Country" = 'Somalia';
UPDATE accounts SET "Country" = 'SR' WHERE "Country" = 'Suriname';
UPDATE accounts SET "Country" = 'SS' WHERE "Country" = 'South Sudan';
UPDATE accounts SET "Country" = 'ST' WHERE "Country" = 'Sao Tome and Principe';
UPDATE accounts SET "Country" = 'SV' WHERE "Country" = 'El Salvador';
UPDATE accounts SET "Country" = 'SX' WHERE "Country" = 'Sint Maarten (Dutch part)';
UPDATE accounts SET "Country" = 'SY' WHERE "Country" = 'Syrian Arab Republic';
UPDATE accounts SET "Country" = 'SZ' WHERE "Country" = 'Swaziland';
UPDATE accounts SET "Country" = 'TC' WHERE "Country" = 'Turks and Caicos Islands';
UPDATE accounts SET "Country" = 'TD' WHERE "Country" = 'Chad';
UPDATE accounts SET "Country" = 'TF' WHERE "Country" = 'French Southern Territories';
UPDATE accounts SET "Country" = 'TG' WHERE "Country" = 'Togo';
UPDATE accounts SET "Country" = 'TH' WHERE "Country" = 'Thailand';
UPDATE accounts SET "Country" = 'TJ' WHERE "Country" = 'Tajikistan';
UPDATE accounts SET "Country" = 'TK' WHERE "Country" = 'Tokelau';
UPDATE accounts SET "Country" = 'TL' WHERE "Country" = 'Timor-Leste';
UPDATE accounts SET "Country" = 'TM' WHERE "Country" = 'Turkmenistan';
UPDATE accounts SET "Country" = 'TN' WHERE "Country" = 'Tunisia';
UPDATE accounts SET "Country" = 'TO' WHERE "Country" = 'Tonga';
UPDATE accounts SET "Country" = 'TR' WHERE "Country" = 'Turkey';
UPDATE accounts SET "Country" = 'TT' WHERE "Country" = 'Trinidad and Tobago';
UPDATE accounts SET "Country" = 'TV' WHERE "Country" = 'Tuvalu';
UPDATE accounts SET "Country" = 'TW' WHERE "Country" IN ('Taiwan', 'Taiwan, Province of China');
UPDATE accounts SET "Country" = 'TZ' WHERE "Country" = 'United Republic of Tanzania';
UPDATE accounts SET "Country" = 'UA' WHERE "Country" = 'Ukraine';
UPDATE accounts SET "Country" = 'UG' WHERE "Country" = 'Uganda';
UPDATE accounts SET "Country" = 'UY' WHERE "Country" = 'Uruguay';
UPDATE accounts SET "Country" = 'UZ' WHERE "Country" = 'Uzbekistan';
UPDATE accounts SET "Country" = 'VA' WHERE "Country" = 'Holy See (Vatican City State)';
UPDATE accounts SET "Country" = 'VC' WHERE "Country" = 'Saint Vincent and the Grenadines';
UPDATE accounts SET "Country" = 'VE' WHERE "Country" = 'Venezuela';
UPDATE accounts SET "Country" = 'VE' WHERE "Country" = 'Venezuela, Bolivarian Republic of';
UPDATE accounts SET "Country" = 'VG' WHERE "Country" = 'British Virgin Islands';
UPDATE accounts SET "Country" = 'VG' WHERE "Country" = 'Virgin Islands, British';
UPDATE accounts SET "Country" = 'VI' WHERE "Country" = 'US Virgin Islands';
UPDATE accounts SET "Country" = 'VN' WHERE "Country" = 'Vietnam';
UPDATE accounts SET "Country" = 'VU' WHERE "Country" = 'Vanuatu';
UPDATE accounts SET "Country" = 'WF' WHERE "Country" = 'Wallis and Futuna';
UPDATE accounts SET "Country" = 'WS' WHERE "Country" = 'Samoa';
UPDATE accounts SET "Country" = 'XK' WHERE "Country" = 'Kosovo';
UPDATE accounts SET "Country" = 'YE' WHERE "Country" = 'Yemen';
UPDATE accounts SET "Country" = 'YT' WHERE "Country" = 'Mayotte';
UPDATE accounts SET "Country" = 'ZA' WHERE "Country" = 'South Africa';
UPDATE accounts SET "Country" = 'ZM' WHERE "Country" = 'Zambia';
UPDATE accounts SET "Country" = 'ZW' WHERE "Country" = 'Zimbabwe';

-- ============================================================================
-- STEP 3: POST-MIGRATION VERIFICATION
-- ============================================================================

-- 3.1: Check accounts table - should show mostly codes now (ALL records including deleted)
SELECT 
    CASE 
        WHEN LENGTH("Country") = 2 AND "Country" ~ '^[A-Z]{2}$' THEN 'Code (2-letter)'
        WHEN LENGTH("Country") > 2 THEN 'Full Name (STILL EXISTS - CHECK!)'
        WHEN "Country" IS NULL OR "Country" = '' THEN 'Empty/Null'
        ELSE 'Other'
    END as country_type,
    COUNT(*) as count
FROM accounts
GROUP BY country_type
ORDER BY count DESC;

-- 3.2: List any remaining full country names (should be empty or very few)
SELECT DISTINCT "Country", COUNT(*) as count
FROM accounts
WHERE LENGTH("Country") > 2 
GROUP BY "Country"
ORDER BY count DESC;

-- ============================================================================
-- STEP 4: COMMIT OR ROLLBACK
-- ============================================================================

-- If verification looks good, commit:
COMMIT;

-- If something is wrong, rollback:
-- ROLLBACK;

