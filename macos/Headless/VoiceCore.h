// In-process calls to the existing Go Core. No event or frame subprocess IPC.
#ifndef VOICE_REWRITE_CORE_H
#define VOICE_REWRITE_CORE_H
#include <stdint.h>
#include <stddef.h>

int VRDown(int code);
unsigned long long VRPress(int code, int busy);
int VRActivate(int code, unsigned long long token);
int VRRelease(int code);
void VRAbandon(void);
int VRArmed(void);
unsigned long long VREpochStart(void);
int VREpochValid(unsigned long long token);
void VREpochCancel(void);
double VRLevel(double previous, double raw);
int VRWave(int index, double level, unsigned int tick);
unsigned int VRSpinner(int index, unsigned int tick);
char *VRDuration(long long seconds);
void VRFree(void *ptr);
char *VRProfile(const char *config);
char *VRRun(const char *config, const char *operation, const unsigned char *audio,
            int length, const char *selection, unsigned long long token);
#endif
