#include "libnuvolari/nuvolari.h"

#include <stdio.h>
#include <stdlib.h>

int main() {
  const char *settings = "{\"adaptive\": true, \"hostname\": \"127.0.0.1\", \"port\": \"4444\", \"skip_tls_verify\": true}";
  int err = nuvolari_start_download(settings);
  if (err != 0) {
    fprintf(stderr, "nuvolari_start_download() failed\n");
    exit(EXIT_FAILURE);
  }
  for (;;) {
    char *event = nuvolari_get_next_event();
    if (event == NULL) {
      break;
    }
    fprintf(stderr, "event: %s\n", event);
    nuvolari_free_event(event);
  }
  exit(EXIT_SUCCESS);
}
