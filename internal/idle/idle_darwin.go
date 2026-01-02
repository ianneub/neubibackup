//go:build darwin

package idle

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework CoreFoundation -framework IOKit

#include <CoreFoundation/CoreFoundation.h>
#include <IOKit/IOKitLib.h>

// getIdleTimeNanoseconds returns the time since last user input in nanoseconds.
// Returns -1 on error.
int64_t getIdleTimeNanoseconds() {
    io_iterator_t iter;
    io_registry_entry_t entry;
    CFMutableDictionaryRef properties = NULL;
    int64_t idleTime = -1;

    // Get matching services for IOHIDSystem
    kern_return_t result = IOServiceGetMatchingServices(
        kIOMainPortDefault,
        IOServiceMatching("IOHIDSystem"),
        &iter
    );

    if (result != KERN_SUCCESS) {
        return -1;
    }

    // Get the first (and typically only) IOHIDSystem entry
    entry = IOIteratorNext(iter);
    if (entry) {
        // Get all properties from the entry
        result = IORegistryEntryCreateCFProperties(
            entry,
            &properties,
            kCFAllocatorDefault,
            0
        );

        if (result == KERN_SUCCESS && properties != NULL) {
            // Look for HIDIdleTime property
            CFTypeRef idleTimeRef = CFDictionaryGetValue(properties, CFSTR("HIDIdleTime"));
            if (idleTimeRef != NULL) {
                CFTypeID type = CFGetTypeID(idleTimeRef);

                if (type == CFNumberGetTypeID()) {
                    // Property is a CFNumber
                    CFNumberGetValue((CFNumberRef)idleTimeRef, kCFNumberSInt64Type, &idleTime);
                } else if (type == CFDataGetTypeID()) {
                    // Property is CFData (older macOS versions)
                    CFDataRef data = (CFDataRef)idleTimeRef;
                    if (CFDataGetLength(data) >= sizeof(int64_t)) {
                        CFDataGetBytes(data, CFRangeMake(0, sizeof(int64_t)), (UInt8*)&idleTime);
                    }
                }
            }
            CFRelease(properties);
        }
        IOObjectRelease(entry);
    }
    IOObjectRelease(iter);

    return idleTime;
}
*/
import "C"

import (
	"log/slog"
	"time"
)

// getIdleTime returns the duration since the last user input on macOS.
// Uses IOKit to query the HIDIdleTime property from IOHIDSystem.
func getIdleTime() time.Duration {
	ns := C.getIdleTimeNanoseconds()
	if ns < 0 {
		slog.Error("Failed to get idle time from IOKit", "error", "getIdleTimeNanoseconds returned -1")
		// Error: assume user is active (fail-safe)
		return 0
	}
	idleTime := time.Duration(ns)
	slog.Debug("Got idle time from IOKit", "idle_time", idleTime)
	return idleTime
}
