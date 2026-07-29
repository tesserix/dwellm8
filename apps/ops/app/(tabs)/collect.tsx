import React, { useMemo, useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import {
  AppHeader, AvatarButton, Card, Screen, Segmented, SearchBar, ListRow, Avatar,
  StatusPill, Button, Metric, RupeeIcon,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import type { Tone } from '@dwellm8/mobile-shared';
import { arrears } from '../../src/data/mock';

/**
 * Collections worklist.
 *
 * Ordered by what recovers money fastest, not by amount: a failed mandate is
 * one tap from a retry, a broken promise to pay outranks a fresh reminder.
 */

const stageTone: Record<string, Tone> = {
  Reminder: 'blue',
  'Follow-up': 'amber',
  Notice: 'red',
  Escalated: 'violet',
};

const bucketOf = (days: number) => (days <= 7 ? '1–7 days' : days <= 30 ? '8–30 days' : '30+ days');

export default function Collect() {
  const router = useRouter();
  const [tab, setTab] = useState('Arrears');
  const [q, setQ] = useState('');

  const list = useMemo(() => {
    const filtered = arrears.filter(
      (a) =>
        !q ||
        a.tenant.toLowerCase().includes(q.toLowerCase()) ||
        a.unit.toLowerCase().includes(q.toLowerCase()),
    );
    if (tab === 'Mandates') return filtered.filter((a) => a.mandate !== 'Active');
    if (tab === 'Promises') return filtered.filter((a) => a.promiseToPay);
    return filtered.slice().sort((a, b) => b.daysLate - a.daysLate);
  }, [tab, q]);

  const totalDue = list.reduce((sum, a) => sum + a.duePaise, 0);
  const failedMandates = arrears.filter((a) => a.mandate === 'Failed' || a.mandate === 'Paused').length;

  return (
    <>
      <AppHeader
        title="Collections"
        showCaret={false}
        left={<AvatarButton onPress={() => router.push('/profile')} />}
      />
      <Screen>
        <View style={{ marginTop: space(3) }}>
          <Segmented items={['Arrears', 'Mandates', 'Promises']} value={tab} onChange={setTab} />
        </View>

        <View style={s.metrics}>
          <Metric value={inr(totalDue, { noPaise: true })} label="outstanding in view" tone="red" />
          <Metric value={String(failedMandates)} label="mandates need a retry" tone="amber" />
        </View>

        <View style={{ marginTop: space(3) }}>
          <SearchBar value={q} onChange={setQ} placeholder="Tenant or unit" />
        </View>

        <Card padded={false} style={{ paddingHorizontal: space(4) }}>
          {list.map((a, i) => (
            <ListRow
              key={a.id}
              left={<Avatar initials={a.initials} tone={a.daysLate > 20 ? 'red' : 'blue'} />}
              title={`${a.tenant} — ${inr(a.duePaise, { noPaise: true })}`}
              subtitle={a.unit}
              meta={`${a.daysLate} days late · ${bucketOf(a.daysLate)} · ${a.lastContact}`}
              right={
                <View style={{ alignItems: 'flex-end', gap: 6 }}>
                  <StatusPill text={a.stage} tone={stageTone[a.stage]} />
                  {a.mandate !== 'Active' ? (
                    <Text style={s.mandate}>Mandate {a.mandate.toLowerCase()}</Text>
                  ) : null}
                </View>
              }
              onPress={() => router.push(`/arrear?id=${a.id}`)}
              last={i === list.length - 1}
            />
          ))}
          {!list.length ? <Text style={s.empty}>Nothing in this view. Good.</Text> : null}
        </Card>

        <Card>
          <Text style={s.helpTitle}>Collected today</Text>
          <Text style={s.helpBody}>
            {inr(2_26_500_00, { noPaise: true })} across 6 tenancies. Cash and
            transfers you record here post to the ledger immediately and issue a receipt to the tenant.
          </Text>
          <Button
            label="Record a payment"
            icon={<RupeeIcon size={19} c="#FFF" />}
            onPress={() => router.push('/receipt')}
            style={{ marginTop: space(4) }}
          />
        </Card>
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  metrics: { flexDirection: 'row', gap: 10, marginHorizontal: space(4), marginTop: space(3), marginBottom: space(1) },
  mandate: { ...font.small, color: color.negative, fontWeight: '600' },
  empty: { ...font.body, color: color.inkSoft, paddingVertical: space(6), textAlign: 'center' },
  helpTitle: { ...font.h3, color: color.inkStrong },
  helpBody: { ...font.body, color: color.inkSoft, marginTop: 6, lineHeight: 21 },
});
