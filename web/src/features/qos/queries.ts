import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api } from '@/lib/api'

export type Priority = 'high' | 'normal' | 'low'
export interface QosDevice { mac: string; priority?: Priority; download_mbps?: number; upload_mbps?: number }
export interface QosConfig { enabled: boolean; download_mbps?: number; upload_mbps?: number; devices?: QosDevice[] }
export interface QosStatus { enabled: boolean; active: boolean; reason?: string; pending?: string[] }
interface Result { config: QosConfig; status: QosStatus }

export function useQosConfig() {
  return useQuery({ queryKey: ['qos', 'config'], queryFn: () => api.get<QosConfig>('/api/qos/config') })
}
export function useQosStatus() {
  return useQuery({ queryKey: ['qos', 'status'], queryFn: () => api.get<QosStatus>('/api/qos/status'), refetchInterval: 10000 })
}
export function useQosDeviceApply(mac: string) {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (device: QosDevice) => api.put<Result>(`/api/qos/devices/${encodeURIComponent(mac)}`, device),
    onSettled: () => client.invalidateQueries({ queryKey: ['qos'] }),
  })
}

/** One device at a time, with the MAC supplied per call for group actions. */
export function useQosDevicesApply() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (device: QosDevice) => api.put<Result>(`/api/qos/devices/${encodeURIComponent(device.mac)}`, device),
    onSettled: () => client.invalidateQueries({ queryKey: ['qos'] }),
  })
}
