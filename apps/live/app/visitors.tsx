import React, { useState } from 'react';
import { Text, StyleSheet, ActivityIndicator, View } from 'react-native';
import { useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, ListRow, StatusPill, Button, Avatar, Toast, Field,
  ChoiceRow, KeyValue, ActionBar, EmptyState, HouseArt,
  color, font, space,
} from '@dwellm8/mobile-shared';
import type { Tone } from '@dwellm8/mobile-shared';
import { useLiveData, usePasses } from '../src/data/source';

/**
 * Visitors and gate passes — the resident's side of the gate (#246).
 *
 * Pre-approval is the point: a guest who is expected walks in with a code,
 * and nobody at the gate has to decide on the resident's behalf.
 */

const stateTone: Record<string, Tone> = {
  Expected: 'blue', 'At the gate': 'amber', Inside: 'green', Left: 'neutral',
  Denied: 'red', Cancelled: 'neutral',
};

const kinds = [
  { code: 'guest', label: 'Guest' },
  { code: 'delivery', label: 'Delivery' },
  { code: 'cab', label: 'Cab' },
  { code: 'help', label: 'Help' },
];

export default function Visitors() {
  const router = useRouter();
  const { tenancy, leaseId } = useLiveData();
  const { loading, passes, create, cancel } = usePasses(leaseId);
  const [adding, setAdding] = useState(false);
  const [name, setName] = useState('');
  const [kind, setKind] = useState('guest');
  const [busy, setBusy] = useState(false);
  const [toast, setToast] = useState<string | null>(null);

  const say = (m: string) => {
    setToast(m);
    setTimeout(() => setToast(null), 2600);
  };

  const createPass = async () => {
    if (!name.trim() || busy) return;
    setBusy(true);
    try {
      const pass = await create(name.trim(), kind, 24);
      setAdding(false);
      setName('');
      say(`Pass created — code ${pass.code}, share it with them`);
    } catch (err) {
      say((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  if (adding) {
    return (
      <>
        <BackHeader title="Expect someone" onBack={() => setAdding(false)} />
        <Screen>
          {toast ? <Toast text={toast} /> : null}
          <Card>
            <Field label="Who is coming" value={name} onChange={setName} placeholder="Priya Menon" />
            <Text style={s.h}>What kind of visit</Text>
            {kinds.map((k, i) => (
              <ChoiceRow key={k.code} label={k.label} selected={kind === k.code} onPress={() => setKind(k.code)} last={i === 3} />
            ))}
          </Card>
          <Card>
            <KeyValue k="Valid" v="24 hours from creation" />
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
              label={busy ? 'Creating…' : 'Create pass'}
              onPress={createPass}
              disabled={!name.trim() || busy}
              style={{ flex: 1.6 }}
            />
          </ActionBar>
        </Screen>
      </>
    );
  }

  return (
    <>
      <BackHeader title="Visitors" subtitle={tenancy.unit} onBack={() => router.back()} />
      <Screen>
        {toast ? <Toast text={toast} /> : null}

        {loading ? (
          <View style={{ paddingVertical: space(8), alignItems: 'center' }}><ActivityIndicator /></View>
        ) : passes.length === 0 ? (
          <EmptyState
            art={<HouseArt size={160} />}
            title="Nobody expected yet"
            body="Create a pass and your visitor walks in with a code — no calls from the gate."
          />
        ) : (
          <Card padded={false} style={{ paddingHorizontal: space(4), marginTop: space(3) }}>
            {passes.map((v, i) => (
              <ListRow
                key={v.id}
                left={<Avatar initials={v.name.slice(0, 2).toUpperCase()} tone={stateTone[v.state]} />}
                title={v.name}
                subtitle={`${v.kind} · ${v.when}`}
                meta={v.state === 'Expected' ? `Code ${v.code}` : undefined}
                right={
                  v.state === 'Expected' ? (
                    <Button label="Cancel" tone="secondary" small onPress={() => cancel(v.id).then(() => say('Pass cancelled')).catch((e: Error) => say(e.message))} />
                  ) : (
                    <StatusPill text={v.state} tone={stateTone[v.state]} />
                  )
                }
                onPress={() => {}}
                last={i === passes.length - 1}
              />
            ))}
          </Card>
        )}

        <Button label="Expect someone" onPress={() => setAdding(true)} style={{ marginHorizontal: space(4), marginBottom: space(3) }} />

        <Card>
          <Text style={s.h}>How it works at the gate</Text>
          <Text style={s.body}>
            Your visitor gives the code. The guard sees a name and your flat number — never your
            phone number. Gate check-in for guards arrives with the gate leg; until then the code
            is your instruction on the record.
          </Text>
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
