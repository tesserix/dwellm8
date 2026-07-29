import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, ListRow, StatusPill, Button, Avatar, Toast, Field,
  ChoiceRow, KeyValue, ActionBar,
  color, font, space,
} from '@dwellm8/mobile-shared';
import type { Tone } from '@dwellm8/mobile-shared';
import { visitors } from '../src/data/mock';

/**
 * Visitors and gate passes — the resident's side of the gate.
 *
 * Pre-approval is the point: a guest who is expected walks in with a code,
 * and nobody at the gate has to decide on the resident's behalf.
 */

const stateTone: Record<string, Tone> = {
  Expected: 'blue', 'At the gate': 'amber', Inside: 'green', Left: 'neutral', Denied: 'red',
};

export default function Visitors() {
  const router = useRouter();
  const [adding, setAdding] = useState(false);
  const [name, setName] = useState('');
  const [kind, setKind] = useState('Guest');
  const [toast, setToast] = useState<string | null>(null);
  const [decided, setDecided] = useState<Record<string, string>>({});

  const say = (m: string) => {
    setToast(m);
    setTimeout(() => setToast(null), 2600);
  };

  if (adding) {
    return (
      <>
        <BackHeader title="Expect someone" onBack={() => setAdding(false)} />
        <Screen>
          <Card>
            <Field label="Who is coming" value={name} onChange={setName} placeholder="Priya Menon" />
            <Text style={s.h}>What kind of visit</Text>
            {['Guest', 'Delivery', 'Cab', 'Help'].map((k, i) => (
              <ChoiceRow key={k} label={k} selected={kind === k} onPress={() => setKind(k)} last={i === 3} />
            ))}
          </Card>
          <Card>
            <KeyValue k="Valid" v="Today, 4 hours from arrival" />
            <KeyValue k="Code" v="Generated when you save" />
            <KeyValue k="Gate sees" v="Name and flat number only" last />
            <Text style={s.note}>
              The gate never sees your phone number. If your visitor arrives without the code, the
              guard asks you first.
            </Text>
          </Card>
          <ActionBar>
            <Button label="Cancel" tone="secondary" onPress={() => setAdding(false)} style={{ flex: 1 }} />
            <Button
              label="Create pass"
              onPress={() => { setAdding(false); setName(''); say('Pass created — code 6620, share it with them'); }}
              disabled={!name}
              style={{ flex: 1.6 }}
            />
          </ActionBar>
        </Screen>
      </>
    );
  }

  return (
    <>
      <BackHeader title="Visitors" subtitle="Brigade Palm Grove" onBack={() => router.back()} />
      <Screen>
        {toast ? <Toast text={toast} /> : null}

        <Card padded={false} style={{ paddingHorizontal: space(4), marginTop: space(3) }}>
          {visitors.map((v, i) => {
            const state = decided[v.id] ?? v.state;
            return (
              <ListRow
                key={v.id}
                left={<Avatar initials={v.name.slice(0, 2).toUpperCase()} tone={stateTone[state]} />}
                title={v.name}
                subtitle={`${v.kind} · ${v.when}`}
                meta={v.code ? `Code ${v.code}` : undefined}
                right={
                  state === 'At the gate' ? (
                    <View style={{ flexDirection: 'row', gap: 6 }}>
                      <Button label="No" tone="secondary" small onPress={() => { setDecided((d) => ({ ...d, [v.id]: 'Denied' })); say('Turned away'); }} />
                      <Button label="Let in" small onPress={() => { setDecided((d) => ({ ...d, [v.id]: 'Inside' })); say('The guard has been told'); }} />
                    </View>
                  ) : (
                    <StatusPill text={state} tone={stateTone[state]} />
                  )
                }
                onPress={() => {}}
                last={i === visitors.length - 1}
                tone={state === 'At the gate' ? 'amber' : undefined}
              />
            );
          })}
        </Card>

        <Button label="Expect someone" onPress={() => setAdding(true)} style={{ marginHorizontal: space(4), marginBottom: space(3) }} />

        <Card>
          <Text style={s.h}>Daily help</Text>
          <Text style={s.body}>
            Lakshmi comes daily at 07:00 and is marked in and out automatically at the gate. You can
            see her attendance for the month, which is what most disputes about wages come down to.
          </Text>
          <Button label="See attendance" tone="secondary" onPress={() => say('22 days present this month')} style={{ marginTop: space(4) }} />
        </Card>
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(1) },
  body: { ...font.body, color: color.inkSoft, marginTop: space(2), lineHeight: 21 },
  note: { ...font.small, color: color.inkSoft, marginTop: space(3), lineHeight: 18 },
});
