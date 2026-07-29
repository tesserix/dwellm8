import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, ListRow, StatusPill, Button, KeyValue, Toast, Metric,
  color, font, inr, space, PLATFORM_FEE_PCT,
} from '@dwellm8/mobile-shared';
import { payouts } from '../src/data/mock';

/**
 * Owner payouts.
 *
 * The single point where the platform fee is charged (requirements §5.5): it
 * is deducted here, once, from the manager's payout — never added to a
 * tenant's payable and never charged twice.
 */

export default function Payouts() {
  const router = useRouter();
  const [released, setReleased] = useState<string[]>([]);
  const [toast, setToast] = useState<string | null>(null);
  const [open, setOpen] = useState<string | null>(payouts[0].id);

  const release = (id: string, owner: string) => {
    setReleased((r) => [...r, id]);
    setToast(`Payout to ${owner} released — UTR will appear within 2 hours`);
    setTimeout(() => setToast(null), 3000);
  };

  const ready = payouts.filter((p) => p.state.startsWith('Ready') && !released.includes(p.id));
  const readyTotal = ready.reduce((a, p) => a + p.netPaise, 0);

  return (
    <>
      <BackHeader title="Owner payouts" subtitle="Fortnightly cycle · 29 July" onBack={() => router.back()} />
      <Screen>
        {toast ? <Toast text={toast} /> : null}

        <View style={s.metrics}>
          <Metric value={String(ready.length)} label="ready to release" tone="green" />
          <Metric value={inr(readyTotal, { noPaise: true })} label="net to owners" tone="blue" />
          <Metric value="2" label="blocked" tone="red" />
        </View>

        {payouts.map((p) => {
          const isReleased = released.includes(p.id);
          const blocked = p.state.startsWith('Blocked');
          const isOpen = open === p.id;
          return (
            <Card key={p.id} padded={false} style={{ paddingHorizontal: space(4), paddingVertical: space(2) }}>
              <ListRow
                title={p.owner}
                subtitle={p.propertyName}
                meta={`Due ${p.due}`}
                right={
                  <StatusPill
                    text={isReleased ? 'Released' : blocked ? 'Blocked' : 'Ready'}
                    tone={isReleased ? 'green' : blocked ? 'red' : 'blue'}
                  />
                }
                onPress={() => setOpen(isOpen ? null : p.id)}
                last
              />
              {isOpen ? (
                <View style={{ paddingBottom: space(3) }}>
                  {blocked ? (
                    <View style={s.warn}>
                      <Text style={s.warnText}>{p.state}. Nothing can be released until this clears.</Text>
                    </View>
                  ) : (
                    <>
                      <KeyValue k="Rent collected" v={inr(p.grossPaise)} tone="green" />
                      <KeyValue k={`Platform fee (${PLATFORM_FEE_PCT}%)`} v={`− ${inr(p.feePaise)}`} tone="red" />
                      <KeyValue k="Management fee (8%)" v={`− ${inr(Math.round(p.grossPaise * 0.08))}`} tone="red" />
                      <KeyValue k="Net to owner" v={inr(p.netPaise - Math.round(p.grossPaise * 0.08))} last />
                      <Button
                        label={isReleased ? 'Released' : 'Release payout'}
                        onPress={() => release(p.id, p.owner)}
                        disabled={isReleased}
                        style={{ marginTop: space(3) }}
                      />
                    </>
                  )}
                </View>
              ) : null}
            </Card>
          );
        })}

        <Card>
          <Text style={s.h}>Where the fee is charged</Text>
          <Text style={s.body}>
            dwellm8 takes {PLATFORM_FEE_PCT}% once, here, deducted from the payout. Instrument costs —
            a card surcharge, an NEFT charge — are passed through separately and shown before anyone
            pays. A tenant paying by UPI is never charged a platform fee.
          </Text>
        </Card>
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  metrics: { flexDirection: 'row', gap: 10, marginHorizontal: space(4), marginTop: space(4), marginBottom: space(3) },
  h: { ...font.h3, color: color.inkStrong },
  body: { ...font.body, color: color.inkSoft, marginTop: space(2), lineHeight: 21 },
  warn: { backgroundColor: '#FBE6E4', borderRadius: 10, padding: space(3), marginTop: space(2) },
  warnText: { ...font.small, color: '#C0433D', lineHeight: 18 },
});
