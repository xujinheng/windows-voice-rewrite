// Native C declarations for the one Go Core command/event/state contract.
// The OS adapter passes physical facts; Go owns the business transitions.
#ifndef VOICE_REWRITE_CORE_H
#define VOICE_REWRITE_CORE_H
#include <stdint.h>
#include <stddef.h>

enum {
    VR_COMMAND_BIND = 1,
    VR_COMMAND_PRESS,
    VR_COMMAND_ACTIVATE,
    VR_COMMAND_RELEASE,
    VR_COMMAND_ABANDON,
    VR_COMMAND_START,
    VR_COMMAND_CANCEL,
    VR_COMMAND_SHUTDOWN
};
enum {
    VR_EVENT_REJECTED = 0,
    VR_EVENT_BOUND,
    VR_EVENT_PRESSED,
    VR_EVENT_ACTIVATED,
    VR_EVENT_RELEASED,
    VR_EVENT_ABANDONED,
    VR_EVENT_STARTED,
    VR_EVENT_CANCELED,
    VR_EVENT_CLOSED,
    VR_EVENT_PROCESSING,
    VR_EVENT_TRANSCRIBED,
    VR_EVENT_FINISHED,
    VR_EVENT_FAILED
};
typedef struct {
    int kind;
    uint64_t token;
    int role;
    int release;
} VRCommandEvent;
typedef struct {
    int role, held_role, down, armed, can_activate, valid;
    uint64_t current;
    int phase, closed;
} VRCoreState;

VRCommandEvent VRCommand(int kind, int code, uint64_t token, int busy);
VRCoreState VRSnapshot(int code, uint64_t token);
enum {
    VR_DELIVERY_DROP = 0,
    VR_DELIVERY_INSERT,
    VR_DELIVERY_REPLACE,
    VR_DELIVERY_PREVIEW
};
int VRDelivery(uint64_t token, const char *route,
               int target_current, int selection_available, int failed);
int VRClipboardSnapshotFits(unsigned long long size, unsigned long long total);
double VRLevel(double previous, double raw);
int VRWave(int index, double level, unsigned int tick);
unsigned int VRSpinner(int index, unsigned int tick);
void VRDuration(long long seconds, char *buffer, int capacity);
void VRFree(void *ptr);
char *VRLatestResult(void);
char *VRProfile(const char *config);
typedef void (*VREventCallback)(int kind, uint64_t token);
char *VRRun(int rewrite, const unsigned char *audio,
            int length, const char *selection, uint64_t token, VREventCallback onEvent);
#endif
