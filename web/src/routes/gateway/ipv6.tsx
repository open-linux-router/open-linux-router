import { SubPage } from "@/components/layout/sub-page";
import { SwitchField } from "@/components/ui/editable-field";
import { ApplyOutcome, useGatewayEditor } from "@/features/gateway/editor";
import { gatewayChange } from "@/features/gateway/queries";

/**
 * Whether IPv6 passes through this router — one switch, and only this one.
 *
 * Nothing else on any page turns it on, and it turns nothing else on: a network
 * with an IPv6 prefix does not flip it, and flipping it does not give any
 * network a prefix. What it costs is the daemon's to say, and it does — the
 * plan comes back disruptive, naming the interfaces, when turning it on would
 * take this router's own IPv6 away.
 */
export function GatewayIPv6Page() {
  const { config, busy, change, applier, gate } = useGatewayEditor();

  if (!config) return gate;
  const setting = config.ipv6_forwarding;

  return (
    <SubPage section="/gateway" slug="ipv6">
      <ApplyOutcome applier={applier} />

      <SwitchField
        id="gateway-ipv6-forwarding"
        label="Forward IPv6"
        hint={
          setting === undefined
            ? "Not managed yet — this router’s own setting is left as it is. Switching this on or off hands it to olr."
            : "Lets IPv6 pass between this router’s networks and the internet. Turning it on stops this router learning its own IPv6 route from upstream on any interface whose accept_ra is not 2; you will be asked first if that applies."
        }
        checked={setting === true}
        busy={busy}
        onChange={(on) =>
          change(gatewayChange.settings({ ipv6_forwarding: on }))
        }
      />

      {!config.enabled && (
        <p className="text-sm text-muted-foreground">
          The gateway is switched off, so this is saved but not written.
        </p>
      )}
    </SubPage>
  );
}
