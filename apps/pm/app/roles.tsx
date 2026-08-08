import React, { useState } from 'react';
import { View, Text, StyleSheet, ActivityIndicator } from 'react-native';
import {
  BackHeader, Button, Card, Screen, Field, SwitchRow, ErrorState, KeyValue, Toast,
  color, font, space, useBack,
} from '@dwellm8/mobile-shared';
import { useTeam } from '../src/data/team';

/**
 * The jobs a firm defines (#353): what each may do, and how many buildings it
 * carries. The vocabulary is the platform's own — a role that could do
 * something no mandate confers would be a promise nothing keeps.
 */

const permissions: { key: string; label: string }[] = [
  { key: 'property.read', label: 'See the properties under management' },
  { key: 'property.write', label: 'Onboard and edit properties' },
  { key: 'lease.read', label: 'See tenancies and their papers' },
  { key: 'lease.write', label: 'Start, renew and end tenancies' },
  { key: 'money.read', label: 'See rent, arrears and statements' },
  { key: 'money.collect', label: 'Record collections and issue receipts' },
  { key: 'money.payout', label: 'Release payouts to owners' },
  { key: 'maintenance.read', label: 'See maintenance jobs' },
  { key: 'maintenance.write', label: 'Raise and close maintenance jobs' },
  { key: 'document.read', label: 'Read the filing cabinet' },
  { key: 'document.write', label: 'File and issue documents' },
  { key: 'community.read', label: 'Read notices and community posts' },
  { key: 'community.write', label: 'Post notices to residents' },
];

// The band the service enforces; the middle of it is what a firm can maintain.
const minLimit = 1;
const maxLimit = 50;

export default function Roles() {
  const goBack = useBack('/team');
  const team = useTeam();

  const [name, setName] = useState('');
  const [limit, setLimit] = useState('6');
  const [held, setHeld] = useState<string[]>([]);
  const [refused, setRefused] = useState<string | null>(null);
  const [toast, setToast] = useState<string | null>(null);

  const toggle = (key: string) => setHeld((on) =>
    (on.includes(key) ? on.filter((k) => k !== key) : [...on, key]));

  const save = async () => {
    const carries = Number(limit || 0);
    if (!name.trim()) {
      setRefused('A role needs a name.');
      return;
    }
    if (!held.length) {
      setRefused('A role that may do nothing is not a role — say what it covers.');
      return;
    }
    if (carries < minLimit || carries > maxLimit) {
      setRefused(`A role carries between ${minLimit} and ${maxLimit} properties.`);
      return;
    }
    setRefused(null);
    try {
      await team.saveRole({
        name: name.trim(),
        // In the platform's own order, so two firms defining the same role agree.
        permissions: permissions.filter((p) => held.includes(p.key)).map((p) => p.key),
        property_limit: carries,
      });
      setName('');
      setHeld([]);
      setToast('The role is saved');
      setTimeout(() => setToast(null), 2600);
    } catch (err) {
      setRefused((err as Error).message);
    }
  };

  return (
    <>
      <BackHeader title="Roles" subtitle="What a colleague may do, and how much they carry" onBack={goBack} />
      <Screen>
        {toast ? <Toast text={toast} /> : null}
        {team.loading ? <View style={s.waiting}><ActivityIndicator /></View> : null}
        {team.error ? <ErrorState error={team.error} onRetry={team.reload} /> : null}

        {team.roles.length ? (
          <Card>
            <Text style={s.h}>Defined already</Text>
            {team.roles.map((r, i) => (
              <KeyValue
                key={r.id}
                k={r.name}
                v={`Carries up to ${r.property_limit} properties · ${r.people ?? 0} people`}
                last={i === team.roles.length - 1}
              />
            ))}
          </Card>
        ) : null}

        {refused ? <Text style={s.refused} accessibilityRole="alert">{refused}</Text> : null}

        <Card>
          <Text style={s.h}>A new role</Text>
          <Field label="Role name" value={name} onChange={setName} placeholder="Field executive" />
          <Field
            label="Properties this role carries"
            value={limit}
            onChange={setLimit}
            keyboardType="numeric"
            placeholder="6"
          />
          <Text style={s.sub}>
            Five to eight is what one person can walk in a week. One colleague can be given more
            than their role allows, when the firm decides it.
          </Text>
        </Card>

        <Card>
          <Text style={s.h}>What it may do</Text>
          {permissions.map((p, i) => (
            <SwitchRow
              key={p.key}
              label={p.label}
              value={held.includes(p.key)}
              onChange={() => toggle(p.key)}
              last={i === permissions.length - 1}
            />
          ))}
        </Card>

        <View style={s.actions}>
          <Button label="Save the role" onPress={save} style={{ flex: 1 }} />
        </View>
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  waiting: { paddingVertical: space(8), alignItems: 'center' },
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(3) },
  sub: { ...font.small, color: color.inkSoft, marginTop: 3, lineHeight: 18 },
  refused: {
    ...font.small, color: color.negative,
    marginHorizontal: space(4), marginTop: space(4), lineHeight: 18,
  },
  actions: { flexDirection: 'row', marginHorizontal: space(4), marginTop: space(4) },
});
