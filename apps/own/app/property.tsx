import React, { useState } from 'react';
import { View, Text, StyleSheet, Pressable, ScrollView } from 'react-native';
import { useRouter, useLocalSearchParams } from 'expo-router';
import { SafeAreaView } from 'react-native-safe-area-context';
import { documents, inr, properties } from '../src/data/mock';
import {
  BathIcon,
  BedIcon,
  CarIcon,
  Card,
  ChatIcon,
  ChevronRight,
  CloseIcon,
  DocIcon,
  DottedRule,
  GlobeIcon,
  LinkRow,
  MailIcon,
  MoneyRow,
  PhoneIcon,
  PinIcon,
  Segmented,
  color,
  font,
  radius,
  space, useBack,
} from '@dwellm8/mobile-shared';

export default function PropertySheet() {
  const router = useRouter();
  const goBack = useBack('/(tabs)');
  const params = useLocalSearchParams<{ tab?: string }>();
  const [tab, setTab] = useState(params.tab || 'Details');
  const p = properties[0];

  return (
    <View style={{ flex: 1, backgroundColor: color.bgTop }}>
      <SafeAreaView edges={['top']} style={{ backgroundColor: '#FFF' }}>
        <View style={{ padding: space(4), paddingBottom: space(3) }}>
          <Pressable onPress={goBack} hitSlop={10} style={{ marginBottom: space(3) }}>
            <CloseIcon size={26} w={2.2} />
          </Pressable>
          <Text style={s.title}>{p.address}</Text>
          <Text style={s.sub}>{p.locality}</Text>
        </View>
        <View style={{ paddingBottom: space(3) }}>
          <Segmented items={['Details', 'Contact', 'Documents']} value={tab} onChange={setTab} />
        </View>
      </SafeAreaView>

      <ScrollView contentContainerStyle={{ paddingVertical: space(4), paddingBottom: space(10) }}>
        {tab === 'Details' ? (
          <>
            <Card>
              <View style={s.attrs}>
                <Attr icon={<BedIcon size={22} c={color.inkSoft} />} value={p.beds} />
                <Attr icon={<BathIcon size={22} c={color.inkSoft} />} value={p.baths} />
                <Attr icon={<CarIcon size={22} c={color.inkSoft} />} value={p.parking} />
              </View>
              <DottedRule />
              <MoneyRow label="Monthly rent" value={inr(p.rentPaise)} />
              <MoneyRow label="Rent paid to" value={p.paidTo} tone="positive" />
              <MoneyRow label="Lease expires" value={p.leaseExpires} />
              <MoneyRow label="Managed by" value={p.agency} last />
            </Card>
            <Card>
              <Text style={s.blockTitle}>Compliance</Text>
              <MoneyRow label="Fire safety NOC" value="Valid to 06 Feb 2027" tone="positive" />
              <MoneyRow label="Lift AMC" value="Valid to 23 Dec 2026" tone="positive" />
              <MoneyRow label="Electrical safety" value="Renew by 08 Sep 2026" tone="negative" last />
            </Card>
          </>
        ) : null}

        {tab === 'Contact' ? (
          <>
            <Card>
              <View style={s.pmRow}>
                <View style={s.pmAvatar}><Text style={s.pmInit}>RN</Text></View>
                <View>
                  <Text style={s.pmName}>{p.manager}</Text>
                  <Text style={s.pmRole}>{p.managerRole}</Text>
                </View>
              </View>
            </Card>
            <Text style={s.section}>Your agency</Text>
            <Card>
              <Text style={s.agency}>{p.agency}</Text>
              <DottedRule />
              <LinkRow label="Chat" icon={<ChatIcon size={20} />} onPress={() => router.push('/thread')} />
              <LinkRow label="Website" value="anchorpropertycare.in" icon={<GlobeIcon size={20} />} />
              <LinkRow label="Phone number" value="+91 80 4123 8890" icon={<PhoneIcon size={20} />} />
              <LinkRow label="Email" value="care@anchorpropertycare.in" icon={<MailIcon size={20} />} />
              <LinkRow label="Address" value="4th Floor, Prestige Featherlite, Whitefield, Bengaluru 560066" icon={<PinIcon size={20} />} last />
            </Card>
          </>
        ) : null}

        {tab === 'Documents' ? (
          <Card>
            {documents.map((d, i) => (
              <View key={d.id}>
                <Pressable style={s.doc}>
                  <DocIcon size={22} c={color.inkFaint} />
                  <View style={{ flex: 1 }}>
                    <Text style={s.docName}>{d.name}</Text>
                    <Text style={s.docDate}>{d.date}</Text>
                  </View>
                  <ChevronRight size={20} c={color.inkFaint} />
                </Pressable>
                {i < documents.length - 1 ? <DottedRule /> : null}
              </View>
            ))}
          </Card>
        ) : null}
      </ScrollView>
    </View>
  );
}

const Attr = ({ icon, value }: { icon: React.ReactNode; value: number }) => (
  <View style={{ flexDirection: 'row', alignItems: 'center', gap: 6 }}>
    {icon}
    <Text style={s.attrValue}>{value}</Text>
  </View>
);

const s = StyleSheet.create({
  title: { ...font.h1, color: color.inkStrong },
  sub: { ...font.body, color: color.inkSoft, marginTop: 4 },
  attrs: { flexDirection: 'row', gap: 22, paddingBottom: space(3) },
  attrValue: { ...font.h3, color: color.inkStrong },
  blockTitle: { ...font.h3, color: color.inkStrong, marginBottom: space(2) },
  pmRow: { flexDirection: 'row', alignItems: 'center', gap: 14 },
  pmAvatar: {
    width: 62, height: 62, borderRadius: 31, backgroundColor: '#DCE9F2',
    alignItems: 'center', justifyContent: 'center',
  },
  pmInit: { ...font.h3, color: color.accentDeep },
  pmName: { ...font.h3, color: color.inkStrong },
  pmRole: { ...font.body, color: color.inkSoft, marginTop: 2 },
  section: { ...font.h2, color: color.inkStrong, marginHorizontal: space(4), marginTop: space(4), marginBottom: space(3) },
  agency: { ...font.h2, color: color.inkStrong, textAlign: 'center', paddingVertical: space(3) },
  doc: { flexDirection: 'row', alignItems: 'center', gap: 12, paddingVertical: space(4) },
  docName: { ...font.body, color: color.inkStrong, fontWeight: '600' },
  docDate: { ...font.small, color: color.inkSoft, marginTop: 3 },
});
