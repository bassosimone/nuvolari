# Nuvolari

This repository contains several NDTv7 clients. NDTv7 is a non backwards
compatible redesign of [the NDT protocol](
https://github.com/ndt-project/ndt/wiki/NDTProtocol). In particular we
redesigned the NDT protocol to work natively and only over WebSocket and TLS,
so to remove the complexity induced by trying to be backward compatible with
[NDT's original implementation](https://github.com/ndt-project/ndt).

This repository is called nuvolari because it is the companion package of
the NDT7 server implementation, included in [m-lab/ndt-cloud](
https://github.com/m-lab/ndt-cloud). You can [translate "cloud" to "nuvola"](
https://translate.google.com/#it/en/nuvola) in Italian. Also
[Tazio Nuvolari](https://en.wikipedia.org/wiki/Tazio_Nuvolari) was a fast
and brave Formula One driver in the late fourties and we aim for this
implementation to be also fast to read and efficient. Also Nuvolari was
_basso di statura_ (i.e. not so high) and also [@bassosimone](
github.com/bassosimone)'s surname means "not so high" in Italian,
hence the name sounds perfect.

This is the repository's anthem:

[![Nuvolari](https://img.youtube.com/vi/56kHVXVQOb0/0.jpg)](
https://www.youtube.com/watch?v=56kHVXVQOb0).

This repository contains the following NDTv7 client implementations:

- [a Go client implementation](golang);

- [a JavaScript client implementation](www).
