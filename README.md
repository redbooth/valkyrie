valkyrie
========

Golang implementation of the Zephyr protocol


License
-------

BSD 3-clause, see accompanying LICENSE file.


Dependencies
------------

Required:
  - [Go](https://golang.org) 1.4+

Optional:
  - [golang-builder](https://github.com/aerofs/golang-builder)
    To build a minimal docker container <br/>
    Also useful to enable support for zero-copy via the `splice` syscall,
    which currently requires a patch to the standard library.


Usage
-----

    ./valkyrie [-port <port>] [-splice=<true|false>]

  - `port`: listening port
  - `splice`: whether to enable use of the `splice` syscall <br/>
     NB: do NOT use if the binary wasn't built with a patched standard library
     as it will significantly increase memory footprint and degrade performance
     due to suboptimal buffer allocation strategy


Protocol
--------

The Zephyr protocol is designed to allow secure communication between clients
that are not able to establish a direct TCP connection.

Peer discovery and connection negotiation are not part of the protocol and must
happen out-of-band.

## Identifier assignment

When a peer opens a connection to a Zephyr relay, it is assigned a random 4
bytes identifier. The relay informs the peer of this assignment through a
single packet of the following form:

       0                   1                   2                   3
       0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
      +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
      |      0x82     |      0x96     |      0x44     |      0xa1     |
      +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
      |      0x00     |      0x00     |      0x00     |      0x04     |
      +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
      |                  Assigned Zephyr Identifier                   |
      +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+

The first four bytes correspond to the Zephyr protocol magic header.

The second four bytes correspond to the length of the payload, which is always
4 in this case, as a 32bit integer encoded in big-endian.

The last four bytes correspond to the payload, in this case Zephyr identifier
assigned by the relay server to the peer, to be used in a subsequent binding
request.


## Binding

To establish a bidirectional stream, an act referred to as "binding" in the
Zephyr terminology, two peers must exchange their respective Zephyr identifiers
out-of-band.

Having exchanged ZIDs, each peer must send a bind request to the relay:

       0                   1                   2                   3
       0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
      +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
      |      0x82     |      0x96     |      0x44     |      0xa1     |
      +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
      |      0x00     |      0x00     |      0x00     |      0x04     |
      +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
      |                  Requested Zephyr Identifier                  |
      +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+

The first four bytes correspond to the Zephyr protocol magic header.

The second four bytes correspond to the length of the payload, which is always
4 in this case, as a 32bit integer encoded in big-endian.

The last four bytes correspond to the payload, in this case the Zephyr
identifier that the client is attempting to bind to.

A Zephyr server MUST close any connection that fails to send a valid bind
request within a reasonable amount of time.

A Zephyr server MUST immediately close any connection through which it receives
a bind request:
  - to an unknown peer
  - to a peer that is already bound
  - to the caller itself (i.e. a self-bind request)

A Zephyr server MUST close any connection through which it receives a valid bind
request that is not matched by the corresponding peer within a reasonable amount
of time.


## Streaming

A zephyr server MUST NOT start forwarding packets until a successful binding is
achieved, that is until both peers have sent matching bind requests.

Once two connections are bound, the zephyr server simply forwards packets in
both direction, oblivious to their contents. Crucially, this allows peers to
communicate with end-to-end encryption, for instance by establishing a
mutually-authenticated TLS session.


## Connection termination

When a connection is closed, the zephyr server MUST immediately close any bound
connection.

