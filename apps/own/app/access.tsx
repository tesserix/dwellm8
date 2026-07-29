import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, ListRow, StatusPill, Button, Avatar, KeyValue,
  Toast, ChoiceRow, Field, ActionBar,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import type { Tone } from '@dwellm8/mobile-shared';
import { access, accessRoles } from '../src/data/mock';

/**
 * People you have given access to.
 *
 * An owner's people are not an agency's staff: there are three or four of
 * them, each needs one slice, and the owner must be able to revoke any of them
 * in a tap. Roles are scoped to this owner's properties and never merge with
 * the managing agency's permissions.
 */

const roleTone: Record<string, Tone> = {
  'Co-owner': 'violet',
  Accountant: 'blue',
  Caretaker: 'amber',
  'View only': 'neutral',
};

export default function Access() {
  const router = useRouter();
  const [people, setPeople] = useState(access);
  const [adding, setAdding] = useState(false);
  const [name, setName] = useState('');
  const [phone, setPhone] = useState('');
  const [role, setRole] = useState(accessRoles[1].name);
  const [toast, setToast] = useState<string | null>(null);

  const say = (m: string) => {
    setToast(m);
    setTimeout(() => setToast(null), 2800);
  };

  if (adding) {
    const picked = accessRoles.find((r) => r.name === role)!;
    return (
      <>
        <BackHeader title="Give someone access" onBack={() => setAdding(false)} />
        <Screen>
          <Card>
            <Field label="Their name" value={name} onChange={setName} placeholder="Ramesh Rout" />
            <Field label="Mobile number" value={phone} onChange={setPhone} placeholder="+91 98450 00000" keyboardType="phone-pad" />
          </Card>

          <Card>
            <Text style={s.h}>What may they do?</Text>
            {accessRoles.map((r, i) => (
              <ChoiceRow
                key={r.name}
                label={r.name}
                hint={r.hint}
                selected={role === r.name}
                onPress={() => setRole(r.name)}
                last={i === accessRoles.length - 1}
              />
            ))}
          </Card>

          <Card>
            <Text style={s.h}>{picked.name} will be able to</Text>
            {picked.can.map((c) => <Text key={c} style={s.can}>· {c}</Text>)}
            <Text style={[s.h, { marginTop: space(4) }]}>And will never</Text>
            {picked.cannot.map((c) => <Text key={c} style={s.cannot}>· {c}</Text>)}
            <Text style={s.note}>
              Your payout account is never visible to anyone you invite, and money can only ever
              leave to the account registered against your name.
            </Text>
          </Card>
        </Screen>

        <ActionBar>
          <Button label="Cancel" tone="secondary" onPress={() => setAdding(false)} style={{ flex: 1 }} />
          <Button
            label="Send invitation"
            disabled={!name || !phone}
            onPress={() => {
              setPeople((p) => [...p, { id: `x${p.length}`, name, phone, role, since: 'Invited just now', state: 'Invited' as const, limitPaise: picked.limitPaise }]);
              setAdding(false); setName(''); setPhone('');
              say(`${name} invited — they get access when they sign in`);
            }}
            style={{ flex: 1.6 }}
          />
        </ActionBar>
      </>
    );
  }

  return (
    <>
      <BackHeader title="People with access" subtitle="Your properties, your people" onBack={() => router.back()} />
      <Screen>
        {toast ? <Toast text={toast} /> : null}

        <Card padded={false} style={{ paddingHorizontal: space(4), marginTop: space(3) }}>
          {people.map((p, i) => (
            <ListRow
              key={p.id}
              left={<Avatar initials={p.name.split(' ').map((w) => w[0]).join('')} tone={roleTone[p.role] ?? 'neutral'} />}
              title={p.name}
              subtitle={`${p.role}${p.limitPaise ? ` · approves up to ${inr(p.limitPaise, { noPaise: true })}` : ''}`}
              meta={`${p.phone} · ${p.since}`}
              right={<StatusPill text={p.state} tone={p.state === 'Active' ? 'green' : 'amber'} />}
              onPress={() => say(`${p.name} — tap and hold to revoke in the real app`)}
              last={i === people.length - 1}
            />
          ))}
        </Card>

        <Button label="Give someone access" onPress={() => setAdding(true)} style={{ marginHorizontal: space(4), marginBottom: space(4) }} />

        <Card>
          <Text style={s.h}>How this differs from your manager</Text>
          <KeyValue k="Your people" v="Invited by you, revoked by you" />
          <KeyValue k="Anchor Property Care" v="Appointed by your management agreement" />
          <KeyValue k="Their staff" v="Managed by the agency, not by you" />
          <KeyValue k="Dwellm8 staff" v="No access to your data without a support request" last />
          <Text style={s.note}>
            Access is scoped to the properties you own. Nobody you invite here can see another
            owner's flat, and nothing you grant changes what your agency's staff can do.
          </Text>
        </Card>
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(1) },
  can: { ...font.body, color: color.positive, marginTop: 6 },
  cannot: { ...font.body, color: color.negative, marginTop: 6 },
  note: { ...font.small, color: color.inkSoft, marginTop: space(3), lineHeight: 18 },
});
