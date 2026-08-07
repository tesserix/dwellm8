import React, { useState } from 'react';
import { View, Text, StyleSheet, ActivityIndicator } from 'react-native';
import { useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, ListRow, StatusPill, Button, Avatar,
  Toast, Metric, ShieldIcon,
  color, font, space, useBack,
} from '@dwellm8/mobile-shared';
import type { Tone } from '@dwellm8/mobile-shared';
import { fmtDate, setPassState, useOpsPasses } from '../src/data/worklists';

/**
 * Gate — the firm's side of #238's passes. Residents pre-approve from the
 * Live app; what happens at the gate is recorded here. Domestic-staff
 * attendance has no schema yet and is not staged.
 */

const stateTone: Record<string, Tone> = {
  expected: 'blue', arrived: 'amber', inside: 'green', left: 'neutral',
  denied: 'red', cancelled: 'neutral',
};

const stateLabel: Record<string, string> = {
  expected: 'Expected', arrived: 'At the gate', inside: 'Inside', left: 'Left',
  denied: 'Denied', cancelled: 'Cancelled',
};

export default function Gate() {
  const router = useRouter();
  const goBack = useBack('/(tabs)');
  const { loading, error, data: passes } = useOpsPasses();
  const [toast, setToast] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const say = (m: string) => {
    setToast(m);
    setTimeout(() => setToast(null), 2200);
  };

  const record = async (id: string, who: string, state: 'arrived' | 'inside' | 'left' | 'denied') => {
    if (busy) return;
    setBusy(true);
    try {
      await setPassState(id, state);
      say(`${who}: ${stateLabel[state].toLowerCase()} recorded`);
    } catch (err) {
      say((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const expected = passes.filter((p) => p.state === 'expected').length;
  const inside = passes.filter((p) => p.state === 'inside').length;
  const atGate = passes.filter((p) => p.state === 'arrived').length;

  return (
    <>
      <BackHeader title="Gate" onBack={goBack} />
      <Screen>
        {toast ? <Toast text={toast} /> : null}

        <View style={s.metrics}>
          <Metric value={loading ? '…' : String(expected)} label="expected" tone="blue" />
          <Metric value={loading ? '…' : String(atGate)} label="at the gate" tone={atGate ? 'amber' : 'green'} />
          <Metric value={loading ? '…' : String(inside)} label="inside" tone="green" />
        </View>

        {loading ? (
          <View style={{ paddingVertical: space(8), alignItems: 'center' }}><ActivityIndicator /></View>
        ) : error ? (
          <Card><Text style={s.body}>{error}</Text></Card>
        ) : passes.length === 0 ? (
          <Card><Text style={s.body}>Nobody expected. Residents pre-approve visitors from the Live app.</Text></Card>
        ) : (
          <Card padded={false} style={{ paddingHorizontal: space(4) }}>
            {passes.map((p, i) => (
              <ListRow
                key={p.pass_id}
                left={<Avatar initials={p.name.slice(0, 2).toUpperCase()} tone={stateTone[p.state] ?? 'neutral'} />}
                title={`${p.name} · ${p.kind}`}
                subtitle={`${p.unit ?? ''}, ${p.property ?? ''} · code ${p.code}`}
                meta={fmtDate(p.valid_from)}
                right={
                  p.state === 'expected' ? (
                    <View style={{ flexDirection: 'row', gap: 6 }}>
                      <Button label="Deny" tone="secondary" small onPress={() => record(p.pass_id, p.name, 'denied')} />
                      <Button label="In" small onPress={() => record(p.pass_id, p.name, 'inside')} />
                    </View>
                  ) : p.state === 'arrived' ? (
                    <View style={{ flexDirection: 'row', gap: 6 }}>
                      <Button label="Deny" tone="secondary" small onPress={() => record(p.pass_id, p.name, 'denied')} />
                      <Button label="In" small onPress={() => record(p.pass_id, p.name, 'inside')} />
                    </View>
                  ) : p.state === 'inside' ? (
                    <Button label="Left" tone="secondary" small onPress={() => record(p.pass_id, p.name, 'left')} />
                  ) : (
                    <StatusPill text={stateLabel[p.state] ?? p.state} tone={stateTone[p.state] ?? 'neutral'} />
                  )
                }
                last={i === passes.length - 1}
                tone={p.state === 'arrived' ? 'amber' : undefined}
              />
            ))}
          </Card>
        )}

        <Card>
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
            <ShieldIcon size={20} />
            <Text style={s.h}>How passes work</Text>
          </View>
          <Text style={s.body}>
            A resident pre-approves a visitor in Dwellm8 Live and the gate sees a name, a flat and
            a code — never a phone number. What you record here lands on the resident's own
            visitors screen the moment you tap it.
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
});
