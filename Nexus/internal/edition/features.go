// Copyright © 2026 BubbleFish Technologies, Inc.
//
// This file is part of BubbleFish Nexus.
//
// BubbleFish Nexus is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// BubbleFish Nexus is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with BubbleFish Nexus. If not, see <https://www.gnu.org/licenses/>.

package edition

// Feature constants. Community edition has none of these enabled.
// Enterprise and TS editions set Current.Features at init time.
const (
	FeatureRBAC             = "rbac"
	FeatureLDAP             = "ldap"
	FeatureSAML             = "saml"
	FeatureOIDC             = "oidc"
	FeatureSplunkAudit      = "splunk_audit"
	FeatureHA               = "ha"
	FeatureMultiTenant      = "multi_tenant"
	FeatureMFA              = "mfa"
	FeatureKeyLifecycle     = "key_lifecycle"
	FeatureIncidentMgmt     = "incident_mgmt"
	FeatureSessionMgmt      = "session_mgmt"
	FeatureCNSA2            = "cnsa2"
	FeatureCAC              = "cac"
	FeatureLMSSigning       = "lms_signing"
	FeatureClassMarking     = "class_marking"
	FeatureBoundaryEnforce  = "boundary_enforce"
	FeatureMediaSanitize    = "media_sanitize"
	FeatureComplianceReport = "compliance_report"
	FeatureAirGapInstall    = "air_gap_install"
	FeatureFIPS             = "fips"
)
