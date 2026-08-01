import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import {
  AppHeader, AvatarButton, Card, Screen, Metric, StatusPill, ListRow, SectionTitle,
  Button, Toast, KeyValue,
  color, font, inr, inrShort, space,
} from '@dwellm8/mobile-shared';
import type { Tone } from '@dwellm8/mobile-shared';
import { admin, alerts, health } from '../../src/data/mock';
import { DemoNotice } from '../../src/components/DemoNotice';

/**
 * Alerts and platform health — the on-call screen.
 *
 * An on-call administrator must learn about a collection outage wherever they
 * are and acknowledge it immediately, so severity, blast radius and the
 * acknowledge action all sit above the fold.
 */

const sevTone: Record<string, Tone> = { P1: 'red', P2: 'amber', P3: 'blue' };

export default function Alerts() {
  const router = useRouter();
  const [acked, setAcked] = useState<string[]>([]);
  const [toast, setToast] = useState<string | null>(null);

  const ack = (id: string, title: string) => {
    setAcked((a) => [...a, id]);
    setToast(`Acknowledged — ${title.slice(0, 34)}…`);
    setTimeout(() => setToast(null), 2600);
  };

  const firing = alerts.filter((a) => a.state === 'Firing' && !acked.includes(a.id));

  return (
    <>
      <AppHeader
        title="Platform health"
        showCaret={false}
        left={<AvatarButton onPress={() => router.push('/profile')} />}
        right={<StatusPill text={admin.onCall ? 'On call' : 'Off call'} tone={admin.onCall ? 'green' : 'neutral'} dot />}
      />
      <Screen>
        <DemoNotice />
        {toast ? <Toast text={toast} /> : null}

        <Card>
          <View style={s.rowBetween}>
            <Text style={s.label}>Status</Text>
            <StatusPill
              text={health.status}
              tone={health.status === 'Healthy' ? 'green' : health.status === 'Degraded' ? 'amber' : 'red'}
              dot
            />
          </View>
          <Text style={s.big}>{inrShort(health.collectionsToday)}</Text>
          <Text style={s.sub}>collected across the platform today · {health.activeOrgs} organisations live</Text>

          <View style={{ flexDirection: 'row', gap: 10, marginTop: space(4) }}>
            <Metric value={`${health.paymentsSuccessPct}%`} label="payment success" tone="green" />
            <Metric value={`${health.mandateSuccessPct}%`} label="mandate success" tone="red" />
            <Metric value={`${health.webhookLagSec}s`} label="webhook lag" tone="amber" />
          </View>
        </Card>

        <SectionTitle>{firing.length ? `${firing.length} firing` : 'Nothing firing'}</SectionTitle>
        {alerts.map((a) => {
          const state = acked.includes(a.id) ? 'Acknowledged' : a.state;
          return (
            <Card key={a.id}>
              <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
                <StatusPill text={a.severity} tone={sevTone[a.severity]} />
                <StatusPill text={state} tone={state === 'Firing' ? 'red' : state === 'Resolved' ? 'green' : 'blue'} dot />
                <View style={{ flex: 1 }} />
                <Text style={s.at}>{a.at}</Text>
              </View>
              <Text style={s.title}>{a.title}</Text>
              <Text style={s.service}>{a.service}</Text>
              <Text style={s.detail}>{a.detail}</Text>

              <View style={{ flexDirection: 'row', gap: 10, marginTop: space(4) }}>
                <Button label="Open" tone="secondary" small onPress={() => router.push(`/alert?id=${a.id}`)} style={{ flex: 1 }} />
                {state === 'Firing' ? (
                  <Button label="Acknowledge" small onPress={() => ack(a.id, a.title)} style={{ flex: 1.2 }} />
                ) : null}
              </View>
            </Card>
          );
        })}

        <SectionTitle>Right now</SectionTitle>
        <Card>
          <KeyValue k="Payouts queued" v={String(health.payoutsQueued)} />
          <KeyValue k="Active users today" v={health.activeUsers.toLocaleString('en-IN')} />
          <KeyValue k="Unmatched bank credits" v={inr(3_18_400_00, { noPaise: true })} tone="amber" />
          <KeyValue k="Open disputes" v="3" last />
          <Button label="Reconciliation" tone="secondary" onPress={() => router.push('/reconcile')} style={{ marginTop: space(4) }} />
        </Card>
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  rowBetween: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' },
  label: { ...font.label, color: color.inkSoft },
  big: { fontSize: 34, fontWeight: '800', color: color.inkStrong, letterSpacing: -0.6, marginTop: 6 },
  sub: { ...font.small, color: color.inkSoft, marginTop: 6 },
  at: { ...font.small, color: color.inkFaint },
  title: { ...font.h3, color: color.inkStrong, marginTop: space(3) },
  service: { ...font.small, color: color.accent, marginTop: 3 },
  detail: { ...font.body, color: color.inkSoft, marginTop: space(2), lineHeight: 21 },
});
