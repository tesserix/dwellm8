import React from 'react';
import { View, Text, StyleSheet, Pressable, ScrollView } from 'react-native';
import { useRouter } from 'expo-router';
import { AppHeader, Card, ChevronLeft, ChevronRight, DocIcon, DottedRule, GlobeIcon, color, font, space } from '@dwellm8/mobile-shared';
import { useLiveData, useMe } from '../src/data/source';

export default function Profile() {
  const router = useRouter();
  const { tenancy } = useLiveData();
  const me = useMe();
  // Identity verifies a phone, not a biography — the tenancy is the name we have.
  const title = tenancy.unit || 'Your tenancy';
  const initials = (tenancy.unit || me.phone || '?').replace(/[^A-Za-z0-9]/g, '').slice(0, 2).toUpperCase() || '?';

  return (
    <View style={{ flex: 1, backgroundColor: color.bgTop }}>
      <AppHeader
        title="Profile"
        showCaret={false}
        left={<Pressable onPress={() => router.back()} hitSlop={10}><ChevronLeft size={28} w={2.4} /></Pressable>}
        right={<Pressable hitSlop={10}><Text style={s.logout}>Log Out</Text></Pressable>}
      />
      <ScrollView contentContainerStyle={{ paddingBottom: space(10) }}>
        <View style={s.hero}>
          <View style={s.avatar}><Text style={s.initials}>{initials}</Text></View>
          <Text style={s.name}>{title}</Text>
          {me.email ? <Text style={s.detail}>{me.email}</Text> : null}
          {me.phone ? <Text style={s.detail}>{me.phone}</Text> : null}
        </View>

        <Text style={s.section}>General</Text>
        <Card>
          <Row icon={<GlobeIcon size={22} />} label="Terms and Conditions" />
          <DottedRule />
          <Row icon={<DocIcon size={22} />} label="Help me" />
        </Card>

        <Pressable style={{ marginTop: space(6) }}>
          <Text style={s.delete}>Delete my account</Text>
        </Pressable>
        <Text style={s.version}>Version 0.1.0 (1)</Text>
      </ScrollView>
    </View>
  );
}

const Row = ({ icon, label }: { icon: React.ReactNode; label: string }) => (
  <Pressable style={s.row}>
    {icon}
    <Text style={s.rowLabel}>{label}</Text>
    <ChevronRight size={20} c={color.inkFaint} />
  </Pressable>
);

const s = StyleSheet.create({
  logout: { ...font.title, color: color.accent },
  hero: { alignItems: 'center', paddingVertical: space(8) },
  avatar: {
    width: 108, height: 108, borderRadius: 54, backgroundColor: '#7B2E86',
    alignItems: 'center', justifyContent: 'center',
  },
  initials: { fontSize: 38, fontWeight: '800', color: '#FFF' },
  name: { ...font.h1, color: color.inkStrong, marginTop: space(4), textAlign: 'center', marginHorizontal: space(4) },
  detail: { ...font.body, color: color.ink, marginTop: 4 },
  section: { ...font.h2, color: color.inkStrong, marginHorizontal: space(4), marginBottom: space(3) },
  row: { flexDirection: 'row', alignItems: 'center', gap: 14, paddingVertical: space(4) },
  rowLabel: { ...font.body, color: color.inkStrong, flex: 1, fontWeight: '600' },
  delete: { ...font.h3, color: '#E0524E', textAlign: 'center' },
  version: { ...font.body, color: color.inkSoft, textAlign: 'center', marginTop: space(6) },
});
