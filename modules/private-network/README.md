# private-network

One private network in exactly one region of a Public Cloud project and one subnet on it, DHCP on,
no gateway (spec 005 T029/T030; research R9, R14). Neither resource carries tags: the name is the
only label, and the `project-network` stage lists both as `unlabelled`.

## CIDR range

`cidr` must be an IPv4 network without host bits, with a prefix length from /16 to /29, lying
inside one RFC 1918 block (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`). The
`network/island` component and the `project-network` stage check the same rule on their own
inputs, so a plan is refused at the level the value enters.

This range is the slice's choice (spec 005 T085/T086), not a documented OVHcloud limit: what the
API admits as a private subnet (other ranges, larger or smaller prefixes, OVHcloud's own
reservations inside a subnet) is **UNVERIFIED until T010** shows it on a live create. The /29
lower limit leaves a DHCP pool beside the network, first host and broadcast addresses (believed).
