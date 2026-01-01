//go:build darwin

package network

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework CoreWLAN -framework CoreLocation -framework Foundation

#import <CoreWLAN/CoreWLAN.h>
#import <CoreLocation/CoreLocation.h>
#import <Foundation/Foundation.h>
#include <stdlib.h>

// Static location manager to persist across calls
static CLLocationManager *locationManager = nil;

// Request location permission - this triggers the macOS permission dialog
// Must be called from the main thread for the dialog to appear
void requestLocationPermission() {
    if (locationManager == nil) {
        locationManager = [[CLLocationManager alloc] init];
    }

    // Check current authorization status
    CLAuthorizationStatus status;
    if (@available(macOS 11.0, *)) {
        status = locationManager.authorizationStatus;
    } else {
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
        status = [CLLocationManager authorizationStatus];
#pragma clang diagnostic pop
    }

    // Only request if not determined yet
    if (status == kCLAuthorizationStatusNotDetermined) {
        // On macOS, we need to start updating location to trigger the permission prompt
        // The prompt appears when we first try to access location data
        [locationManager startUpdatingLocation];
        // Stop immediately - we just needed to trigger the prompt
        [locationManager stopUpdatingLocation];
    }
}

// Check if location services are authorized
// Returns: 0 = not determined, 1 = restricted, 2 = denied, 3 = authorized
int getLocationAuthStatus() {
    if (locationManager == nil) {
        locationManager = [[CLLocationManager alloc] init];
    }

    CLAuthorizationStatus status;
    if (@available(macOS 11.0, *)) {
        status = locationManager.authorizationStatus;
    } else {
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
        status = [CLLocationManager authorizationStatus];
#pragma clang diagnostic pop
    }

    switch (status) {
        case kCLAuthorizationStatusNotDetermined:
            return 0;
        case kCLAuthorizationStatusRestricted:
            return 1;
        case kCLAuthorizationStatusDenied:
            return 2;
        case kCLAuthorizationStatusAuthorizedAlways:
            return 3;
        default:
            return 0;
    }
}

// Returns the current WiFi SSID or NULL if not available
// Note: On macOS 14+ (Sonoma), this requires Location Services permission
// Caller must free the returned string using freeString()
char* getCurrentSSID() {
    @autoreleasepool {
        CWInterface *interface = [[CWWiFiClient sharedWiFiClient] interface];
        if (interface == nil) {
            return NULL;
        }

        NSString *ssid = [interface ssid];
        if (ssid == nil || [ssid length] == 0) {
            return NULL;
        }

        // Return a dynamically allocated copy that Go can free
        return strdup([ssid UTF8String]);
    }
}

void freeString(char* s) {
    if (s != NULL) {
        free(s);
    }
}
*/
import "C"

import (
	"log/slog"
	"sync"
)

var (
	locationRequestOnce sync.Once
)

// RequestLocationPermission requests location permission from the user.
// This should be called once when the app starts if SSID filtering is enabled.
// On macOS 14+, this is required to access the WiFi SSID.
func RequestLocationPermission() {
	locationRequestOnce.Do(func() {
		status := C.getLocationAuthStatus()
		switch status {
		case 0: // Not determined
			slog.Info("Requesting Location Services permission for WiFi SSID detection")
			C.requestLocationPermission()
		case 1: // Restricted
			slog.Warn("Location Services is restricted on this device")
		case 2: // Denied
			slog.Warn("Location Services permission denied - WiFi SSID detection will not work",
				"action", "Enable in System Settings > Privacy & Security > Location Services")
		case 3: // Authorized
			slog.Debug("Location Services permission already granted")
		}
	})
}

// GetCurrentNetwork returns the current WiFi network information on macOS.
// Uses CoreWLAN framework for SSID detection.
//
// On macOS 14 (Sonoma) and later, this requires Location Services permission.
// If permission is not granted, the SSID will be empty/redacted and the function
// returns NetworkStatusUnknown (fail-open behavior - backups will proceed).
//
// To enable SSID detection:
// 1. Open System Settings > Privacy & Security > Location Services
// 2. Enable Location Services
// 3. Find NeubiBackup in the list and enable it
func GetCurrentNetwork() NetworkInfo {
	// Ensure we've requested permission
	RequestLocationPermission()

	ssidPtr := C.getCurrentSSID()
	if ssidPtr == nil {
		// No SSID available - could mean:
		// - Not connected to WiFi
		// - Location permission not granted (SSID redacted)
		// - WiFi is off
		// In all cases, we return Unknown (fail-open)
		slog.Debug("WiFi SSID not available (possibly no Location permission or not connected)")
		return NetworkInfo{Status: NetworkStatusUnknown}
	}
	defer C.freeString(ssidPtr)

	ssid := C.GoString(ssidPtr)
	return NetworkInfo{
		Status: NetworkStatusConnected,
		SSID:   ssid,
	}
}
