#ifndef MEASUREMENT_KIT_NUVOLARI_H
#define MEASUREMENT_KIT_NUVOLARI_H

#include "libnuvolari.h"

#ifdef __cplusplus
extern "C" {
#if __cplusplus >= 201103L
#define NUVOLARI_NOEXCEPT noexcept
#else
#define NUVOLARI_NOEXCEPT throw()
#endif
#else
#define NUVOLARI_NOEXCEPT
#endif

int nuvolari_start_download(const char *settings) NUVOLARI_NOEXCEPT;

char *nuvolari_get_next_event(void) NUVOLARI_NOEXCEPT;

void nuvolari_stop(void) NUVOLARI_NOEXCEPT;

void nuvolari_free_event(char *s) NUVOLARI_NOEXCEPT;

#ifndef NUVOLARI_NO_INLINE_IMPL

int nuvolari_start_download(const char *settings) NUVOLARI_NOEXCEPT {
  char *copy = NULL;
  if (settings != NULL) {
    copy = strdup(settings);
    if (copy == NULL) {
      return 1;
    }
  }
  int rv = nuvolari_start_download_(copy);
  free(copy);
  return rv;
}

char *nuvolari_get_next_event(void) NUVOLARI_NOEXCEPT {
  return nuvolari_get_next_event_();
}

void nuvolari_stop(void) NUVOLARI_NOEXCEPT {
  nuvolari_stop_();
}

void nuvolari_free_event(char *s) NUVOLARI_NOEXCEPT {
  nuvolari_free_event_(s);
}

#endif  /* NUVOLARI_NO_INLINE_IMPL */
#ifdef __cplusplus
}  // extern "C"
#endif
#endif  /* MEASUREMENT_KIT_NUVOLARI_H */
