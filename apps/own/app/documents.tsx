import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, ListRow, SearchBar, Segmented, DocIcon, Button,
  color, font, space,
} from '@dwellm8/mobile-shared';
import { taxPack } from '../src/data/mock';
import { useOwnerDocuments } from '../src/data/source';

/** Everything on file for the properties, in one place a phone can open. */

const AGREEMENTS = [
  { id: 'a1', name: 'Leave and licence agreement — executed.pdf', date: '12 April 2026' },
  { id: 'a2', name: 'Registration receipt — Karnataka.pdf', date: '18 April 2026' },
  { id: 'a3', name: 'Move-in condition report.pdf', date: '15 April 2026' },
  { id: 'a4', name: 'Deposit acknowledgement.pdf', date: '20 April 2026' },
  { id: 'a5', name: 'Police verification acknowledgement.pdf', date: '22 April 2026' },
];

const COMPLIANCE = [
  { id: 'c1', name: 'Fire safety NOC 2026–27.pdf', date: '06 February 2026' },
  { id: 'c2', name: 'Lift AMC — Vertex Elevators.pdf', date: '23 December 2025' },
  { id: 'c3', name: 'Electrical safety certificate 2025.pdf', date: '08 September 2025' },
  { id: 'c4', name: 'Khata certificate.pdf', date: '11 March 2025' },
];

export default function Documents() {
  const router = useRouter();
  const [tab, setTab] = useState('Statements');
  const [q, setQ] = useState('');
  const { documents } = useOwnerDocuments();

  const set =
    tab === 'Statements' ? documents
    : tab === 'Agreements' ? AGREEMENTS
    : tab === 'Compliance' ? COMPLIANCE
    : taxPack.documents;

  const list = set.filter((d) => !q || d.name.toLowerCase().includes(q.toLowerCase()));

  return (
    <>
      <BackHeader title="Documents" subtitle="Everything on file for your properties" onBack={() => router.back()} />
      <Screen>
        <View style={{ marginTop: space(3), marginBottom: space(3) }}>
          <Segmented items={['Statements', 'Agreements', 'Compliance', 'Tax']} value={tab} onChange={setTab} />
        </View>

        <SearchBar value={q} onChange={setQ} placeholder="Search documents" />

        <Card padded={false} style={{ paddingHorizontal: space(4) }}>
          {list.map((d, i) => (
            <ListRow
              key={d.id}
              left={<DocIcon size={20} c={color.inkFaint} />}
              title={d.name}
              subtitle={d.date}
              onPress={() => {}}
              last={i === list.length - 1}
            />
          ))}
          {!list.length ? <Text style={s.empty}>Nothing matches that.</Text> : null}
        </Card>

        <Card>
          <Text style={s.h}>Kept for as long as the law requires</Text>
          <Text style={s.body}>
            Executed agreements and ledger records are retained even if a tenancy ends, because
            statutory retention applies to them. Contact details and inspection photographs are
            deleted on request.
          </Text>
          <Button label="Download everything as a zip" tone="secondary" onPress={() => {}} style={{ marginTop: space(4) }} />
        </Card>
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  h: { ...font.h3, color: color.inkStrong },
  body: { ...font.body, color: color.inkSoft, marginTop: space(2), lineHeight: 21 },
  empty: { ...font.body, color: color.inkSoft, textAlign: 'center', paddingVertical: space(6) },
});
