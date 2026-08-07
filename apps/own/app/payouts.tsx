import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, KeyValue, StatusPill, ListRow, Metric, Button,
  Timeline, PLATFORM_FEE_PCT,
  color, font, space, useBack,
} from '@dwellm8/mobile-shared';
import { inr, payouts, properties } from '../src/data/mock';
import type { Payout } from '../src/data/mock';

/**
 * Where the money went.
 *
 * Every payout opens into its own arithmetic — gross, each deduction, net —
 * because "why is this less than the rent" is the question this screen exists
 * to answer before it is asked.
 */

const tone = (p: Payout) =>
  p.state === 'Paid' ? 'green' : p.state === 'Held' ? 'red' : p.state === 'On the way' ? 'blue' : 'amber';

export default function Payouts() {
  const router = useRouter();
  const goBack = useBack('/(tabs)');
  const [open, setOpen] = useState<string | null>(payouts[0].id);

  const paidThisYear = payouts.filter((p) => p.state === 'Paid').reduce((a, p) => a + p.netPaise, 0);
  const next = payouts.find((p) => p.state !== 'Paid');

  return (
    <>
      <BackHeader title="Payouts" subtitle="Fortnightly, to HDFC ••4471" onBack={goBack} />
      <Screen>
        <View style={s.metrics}>
          <Metric value={inr(paidThisYear, { noPaise: true })} label="paid to you this year" tone="green" />
          <Metric value={next ? inr(next.netPaise, { noPaise: true }) : '—'} label={next ? `next on ${next.date}` : 'nothing due'} tone="amber" />
        </View>

        {payouts.map((p) => {
          const prop = properties.find((x) => x.id === p.propertyId)!;
          const isOpen = open === p.id;
          return (
            <Card key={p.id} padded={false} style={{ paddingHorizontal: space(4), paddingVertical: space(2) }}>
              <ListRow
                title={`${inr(p.netPaise)} — ${p.date}`}
                subtitle={prop.address}
                meta={p.utr ? `UTR ${p.utr}` : p.note}
                right={<StatusPill text={p.state} tone={tone(p)} />}
                onPress={() => setOpen(isOpen ? null : p.id)}
                last
              />
              {isOpen ? (
                <View style={{ paddingBottom: space(3) }}>
                  <KeyValue k="Rent collected" v={inr(p.grossPaise)} tone="green" />
                  {p.repairsPaise ? <KeyValue k="Repairs and maintenance" v={`− ${inr(p.repairsPaise)}`} tone="red" /> : null}
                  <KeyValue k="Management fee (8%)" v={`− ${inr(p.managementPaise)}`} tone="red" />
                  <KeyValue k={`Dwellm8 fee (${PLATFORM_FEE_PCT}%)`} v={`− ${inr(p.platformPaise)}`} tone="red" />
                  <KeyValue k="Paid to you" v={inr(p.netPaise)} last />
                  <View style={{ flexDirection: 'row', gap: 10, marginTop: space(3) }}>
                    <Button label="Open statement" tone="secondary" small onPress={() => router.push('/(tabs)/financials')} style={{ flex: 1 }} />
                    {p.state === 'Paid' ? (
                      <Button label="Download receipt" tone="secondary" small onPress={() => router.push('/documents')} style={{ flex: 1 }} />
                    ) : null}
                  </View>
                </View>
              ) : null}
            </Card>
          );
        })}

        <Card>
          <Text style={s.h}>How a payout is built</Text>
          <Timeline
            items={[
              { at: 'Rent day', what: 'Tenant pays; the receipt is issued the moment it confirms' },
              { at: 'Through the month', what: 'Approved repairs and society dues post against the flat' },
              { at: 'Payout day', what: `Management fee, then the ${PLATFORM_FEE_PCT}% platform fee, are deducted once` },
              { at: 'Within 2 hours', what: 'Money reaches your bank and the UTR appears here', done: false },
            ]}
          />
          <Text style={s.note}>
            You are never charged for a tenant paying by UPI, and the platform fee is charged at this
            one point — not on the tenant, and not twice.
          </Text>
        </Card>
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  metrics: { flexDirection: 'row', gap: 10, marginHorizontal: space(4), marginTop: space(4), marginBottom: space(3) },
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(2) },
  note: { ...font.small, color: color.inkSoft, marginTop: space(4), lineHeight: 18 },
});
