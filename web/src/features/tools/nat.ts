import { t } from '@/lib/i18n'

/** NatTypeTester reports enum names; the page leads with their labels. */
export function natTypeLabel(value: string): string {
  switch (value) {
    case 'open-internet': return t('Open Internet')
    case 'full-cone': return t('Full Cone')
    case 'restricted-cone': return t('Restricted Cone')
    case 'port-restricted-cone': return t('Port Restricted Cone')
    case 'symmetric': return t('Symmetric')
    case 'symmetric-udp-firewall': return t('Symmetric UDP Firewall')
    case 'udp-blocked': return t('UDP Blocked')
    case 'unsupported-server': return t('Unsupported server')
    case 'unknown': return t('Unknown')
    default: return t('Not tested')
  }
}

export function mappingVerdict(value: string): string {
  switch (value) {
    case 'direct': return t('No NAT: the public port is the local port')
    case 'endpoint-independent': return t('One port for every destination')
    case 'address-dependent': return t('Port varies by destination address')
    case 'address-and-port-dependent': return t('Port varies by destination address and port')
    case 'unsupported-server': return t('This server cannot classify')
    default: return t('Not tested')
  }
}

export function filteringVerdict(value: string): string {
  switch (value) {
    case 'endpoint-independent': return t('Any host can reach this port')
    case 'address-dependent': return t('Only hosts this router contacted can reach it')
    case 'address-and-port-dependent': return t('Only the exact host and port this router contacted can reply')
    case 'no changed-source reply': return t('Reachability from outside is unconfirmed')
    case 'unsupported-server': return t('This server cannot classify')
    default: return t('Not tested')
  }
}

/** The raw enum, spaced for reading beside the plain verdict. */
export function behaviorLabel(value: string): string {
  switch (value) {
    case 'direct': return 'Direct'
    case 'endpoint-independent': return 'Endpoint-independent'
    case 'address-dependent': return 'Address-dependent'
    case 'address-and-port-dependent': return 'Address-and-port-dependent'
    case 'unsupported-server': return 'Unsupported server'
    case 'failed': return 'Failed'
    case 'no changed-source reply': return 'No changed-source reply'
    default: return value
  }
}
