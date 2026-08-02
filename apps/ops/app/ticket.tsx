import React, { useState } from 'react';
import { View, Text, StyleSheet, ActivityIndicator } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, KeyValue, StatusPill, Button, Timeline, Toast,
  ActionBar, Field, ChoiceRow, ChatIcon,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import type { Tone } from '@dwellm8/mobile-shared';
import { TICKET_STATUS_LABEL, advanceTicket, fmtDate, fmtTime, useOpsTicket } from '../src/data/worklists';

/**
 * One job — acknowledge, schedule, assess who pays, resolve (#237).
 *
 * The liability call is the point of this screen: the tenant sees the split
 * on their own timeline before any work is approved, which is what stops the
 * argument later.
 */

const statusTone: Record<string, Tone> = {
  open: 'blue', acknowledged: 'violet', scheduled: 'blue',
  in_progress: 'amber', resolved: 'green', cancelled: 'neutral',
};

export default function TicketScreen() {
  const router = useRouter();
  const { id } = useLocalSearchParams<{ id?: string }>();
  const { loading, error, data: t } = useOpsTicket(id);

  const [toast, setToast] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [liability, setLiability] = useState('');
  const [reason, setReason] = useState('');
  const [cost, setCost] = useState('');
  const [slot, setSlot] = useState('');
  const [vendor, setVendor] = useState('');

  const say = (m: string) => {
    setToast(m);
    setTimeout(() => setToast(null), 2600);
  };

  const act = async (action: string, extra?: Omit<Parameters<typeof advanceTicket>[1], 'action'>) => {
    if (!id || busy) return;
    setBusy(true);
    try {
      await advanceTicket(id, { action, ...extra });
      say('Done — the tenant sees this on their timeline');
    } catch (err) {
      say((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  if (loading || !t) {
    return (
      <>
        <BackHeader title="Job" onBack={() => router.back()} />
        <Screen>
          <View style={{ paddingVertical: space(10), alignItems: 'center' }}>
            {loading ? <ActivityIndicator /> : <Text style={s.sub}>{error ?? 'No such job.'}</Text>}
          </View>
        </Screen>
      </>
    );
  }

  const settled = t.status === 'resolved' || t.status === 'cancelled';

  return (
    <>
      <BackHeader title={t.title} subtitle={`${t.unit ?? ''}, ${t.property ?? ''}`} onBack={() => router.back()} />
      <Screen>
        {toast ? <Toast text={toast} /> : null}

        <Card>
          <View style={s.row}>
            <StatusPill text={TICKET_STATUS_LABEL[t.status] ?? t.status} tone={statusTone[t.status] ?? 'neutral'} />
            <Text style={s.cat}>{t.category}</Text>
          </View>
          {t.body ? <Text style={s.detail}>{t.body}</Text> : null}
          <View style={{ marginTop: space(4) }}>
            <KeyValue k="Raised" v={fmtDate(t.raised_at)} />
            {t.slot ? <KeyValue k="Visit" v={t.slot} /> : null}
            {t.vendor ? <KeyValue k="Vendor" v={t.vendor} /> : null}
            {t.cost_minor ? <KeyValue k="Cost" v={inr(t.cost_minor)} last /> : <KeyValue k="Cost" v="Not recorded" last />}
          </View>
          <Button
            label="Message the tenant"
            tone="secondary"
            small
            icon={<ChatIcon size={17} c={color.accent} />}
            onPress={() => router.push(`/thread?id=${t.lease_id}`)}
            style={{ marginTop: space(4) }}
          />
        </Card>

        {!settled ? (
          <Card>
            <Text style={s.h}>Who bears the cost</Text>
            <Text style={s.sub}>
              {t.liability
                ? `Assessed: ${t.liability}-borne. ${t.liability_reason ?? ''}`
                : 'Not assessed yet — the tenant sees "being assessed" until you decide.'}
            </Text>
            {(['owner', 'tenant', 'shared'] as const).map((l, i) => (
              <ChoiceRow
                key={l}
                label={l === 'owner' ? 'Owner' : l === 'tenant' ? 'Tenant' : 'Shared'}
                hint={
                  l === 'owner' ? 'Asset defect or fixture failure — recharged to the owner statement'
                  : l === 'tenant' ? 'Wear, misuse or below the agreement threshold — added to the next invoice'
                  : 'Split by the agreement; both parties see the split before work starts'
                }
                selected={liability === l}
                onPress={() => setLiability(l)}
                last={i === 2}
              />
            ))}
            <Field label="Why — the tenant reads this" value={reason} onChange={setReason} placeholder="Asset defect on an owner-provided fixture…" multiline />
            <Field label="Cost in ₹ (optional)" value={cost} onChange={setCost} placeholder="785" keyboardType="numeric" />
            <Button
              label={busy ? 'Saving…' : 'Record assessment'}
              onPress={() => act('assess', {
                liability, liability_reason: reason,
                cost_minor: cost ? Math.round(Number(cost) * 100) : undefined,
              })}
              disabled={busy || !liability || !reason.trim()}
              style={{ marginTop: space(3) }}
            />
          </Card>
        ) : null}

        {!settled ? (
          <Card>
            <Text style={s.h}>Schedule a visit</Text>
            <Field label="Slot — the tenant sees this" value={slot} onChange={setSlot} placeholder="Thu 7 Aug, 10:00 – 12:00" />
            <Field label="Vendor (optional)" value={vendor} onChange={setVendor} placeholder="Sahyadri Facility Services" />
            <Button
              label={busy ? 'Saving…' : 'Schedule'}
              tone="secondary"
              onPress={() => act('schedule', { slot, vendor })}
              disabled={busy || !slot.trim()}
              style={{ marginTop: space(3) }}
            />
          </Card>
        ) : null}

        <Card>
          <Text style={s.h}>Timeline</Text>
          <Timeline
            items={(t.timeline ?? []).map((e) => ({
              at: `${fmtDate(e.at)}, ${fmtTime(e.at)}`,
              what: e.body,
            }))}
          />
        </Card>
      </Screen>

      {!settled ? (
        <ActionBar>
          {t.status === 'open' ? (
            <Button label="Acknowledge" tone="secondary" onPress={() => act('acknowledge')} disabled={busy} style={{ flex: 1 }} />
          ) : (
            <Button label="Start work" tone="secondary" onPress={() => act('start')} disabled={busy || t.status === 'in_progress'} style={{ flex: 1 }} />
          )}
          <Button label="Resolve" onPress={() => act('resolve')} disabled={busy} style={{ flex: 1 }} />
        </ActionBar>
      ) : null}
    </>
  );
}

const s = StyleSheet.create({
  row: { flexDirection: 'row', alignItems: 'center', gap: 8, justifyContent: 'space-between' },
  cat: { ...font.small, color: color.inkSoft },
  detail: { ...font.body, color: color.ink, lineHeight: 22, marginTop: space(3) },
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(1) },
  sub: { ...font.small, color: color.inkSoft, marginTop: 6, lineHeight: 18, marginBottom: space(2) },
});
