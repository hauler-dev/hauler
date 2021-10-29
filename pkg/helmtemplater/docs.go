// Package helmtemplater
// This is an almost complete copy paste of fleet's helmdeployer package, but repurporsed without the need for fleet's helm fork.
// The modifications include:
// 		* removing the need for fleet's helm fork by removing the custom field on "ForceAdopt"/*
// 		* Removing the majority of the uninstall/install/upgrade helm install logic, since hauler is only using fleet's templating engine/*
package helmtemplater
