import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, Segmented, ListRow, StatusPill, Button, Avatar,
  Toast, Metric, ShieldIcon, KeyIcon,
  color, font, space,
} from '@dwellm8/mobile-shared';
import type { Tone } from '@dwellm8/mobile-shared';
import { domesticStaff, gateLog } from '../src/data/mock';
import type { GateEntry } from '../src/data/mock';

/**
 * Gate — visitors, deliveries and domestic staff attendance.
 *
 * The warden's screen. Approvals are one tap because the person is standing
 * at the gate while this is being read.
 */

const stateTone: Record<string, Tone> = {
  Pending: 'amber', Approved: 'green', Denied: 'red', Inside: 'blue', Left: 'neutral',
};

export default function Gate() {
  const router = useRouter();
  const [tab, setTab] = useState('Gate log');
  const [decided, setDecided] = useState<Record<string, 'Approved' | 'Denied'>>({});
  const [toast, setToast] = useState<string | null>(null);

  const say = (m: string) => {
    setToast(m);
    setTimeout(() => setToast(null), 2200);
  };

  const decide = (id: string, who: string, d: 'Approved' | 'Denied') => {
    setDecided((x) => ({ ...x, [id]: d }));
    say(`${who} ${d.toLowerCase()} — resident notified`);
  };

  const stateOf = (g: (typeof gateLog)[number]): GateEntry['state'] => decided[g.id] ?? g.state;
  const pending = gateLog.filter((g) => stateOf(g) === 'Pending').length;
  const inside = gateLog.filter((g) => g.state === 'Inside').length;

  return (
    <>
      <BackHeader title="Gate" subtitle="Brigade Palm Grove RWA" onBack={() => router.back()} />
      <Screen>
        {toast ? <Toast text={toast} /> : null}

        <View style={s.metrics}>
          <Metric value={String(pending)} label="waiting on approval" tone={pending ? 'amber' : 'green'} />
          <Metric value={String(inside)} label="visitors inside" tone="blue" />
          <Metric value={String(domesticStaff.filter((d) => d.present).length)} label="staff present" tone="green" />
        </View>

        <View style={{ marginBottom: space(3) }}>
          <Segmented items={['Gate log', 'Staff']} value={tab} onChange={setTab} />
        </View>

        {tab === 'Gate log' ? (
          <Card padded={false} style={{ paddingHorizontal: space(4) }}>
            {gateLog.map((g, i) => {
              const state = stateOf(g);
              return (
                <ListRow
                  key={g.id}
                  left={<Avatar initials={g.who.slice(0, 2).toUpperCase()} tone={g.kind === 'Delivery' ? 'violet' : 'blue'} />}
                  title={`${g.who} · ${g.kind}`}
                  subtitle={`${g.detail} — ${g.unit}`}
                  meta={g.at}
                  right={
                    state === 'Pending' ? (
                      <View style={{ flexDirection: 'row', gap: 6 }}>
                        <Button label="Deny" tone="secondary" small onPress={() => decide(g.id, g.who, 'Denied')} />
                        <Button label="Allow" small onPress={() => decide(g.id, g.who, 'Approved')} />
                      </View>
                    ) : (
                      <StatusPill text={state} tone={stateTone[state]} />
                    )
                  }
                  last={i === gateLog.length - 1}
                  tone={state === 'Pending' ? 'amber' : undefined}
                />
              );
            })}
          </Card>
        ) : (
          <Card padded={false} style={{ paddingHorizontal: space(4) }}>
            {domesticStaff.map((d, i) => (
              <ListRow
                key={d.id}
                left={<Avatar initials={d.name.split(' ').map((x) => x[0]).join('')} tone={d.present ? 'green' : 'neutral'} />}
                title={d.name}
                subtitle={`${d.role} · ${d.units}`}
                meta={`In ${d.in} · Out ${d.out}`}
                right={<StatusPill text={d.present ? 'Inside' : 'Out'} tone={d.present ? 'green' : 'neutral'} />}
                last={i === domesticStaff.length - 1}
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
            A resident pre-approves a visitor in dwellm8 Live and the gate sees a code. Anyone who
            arrives unannounced waits here for the resident's answer — the warden never decides on
            their behalf.
          </Text>
          <Button
            label="Issue a gate pass"
            tone="secondary"
            icon={<KeyIcon size={18} c={color.accent} />}
            onPress={() => say('Pass issued — code 8823, valid 2 hours')}
            style={{ marginTop: space(4) }}
          />
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
