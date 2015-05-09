# publisher reconnects brainstorm

In order to make the service 100% more reliable, we need
to add reconnects to busl.

The following is a draft of the protocol specification:

## Clients

1. Clients keep track of the byte offsets, and send them
   as `Content-Range: byte=x-y` headers
2. The client decides how much to buffer, and/or how frequently
   should the posts happen.
3. For every chunk that should be sent, the client sends to
   a new endpoint, which is tentatively defined as `PATCH /streams/<id>`
4. The client will then have to confirm the `ACK` from the server,
   and keep retrying the last post until they get an ack. (For cases
   where the server might be down completely, the client may opt
   to limit the threshold of retries to a certain number N)
5. When the client is done, they'll need to ping an endpoint, tentatively
   defined as `DELETE /streams/<id>` (feel free to suggest a better
   endpoint for this one).

## Servers

1. Servers will make sure that writes to `PATCH` are stored in the DB,
   and will then respond with an `ACK`.
2. If the client sends the same content-range that already exists, the
   server may opt to disregard the message. If the content-range is an
   overlap, the server will have to get the delta, and store it
   accordingly.
3. If the client sends a content-range that seems to imply that some
   data was skipped, the server will just append that data (and assume
   that the client didn't try hard enough).
4. When the client signals `done`, the server closes the connections to
   all existing subscribers to the stream. Reconnects to the stream by
   subscribers with `range` (or `last-event-id` for SSE) that signifies
   a completed stream will get a `204 No Content` response from this point.
   
